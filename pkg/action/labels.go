package action

import (
	"context"
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"os"
	"strconv"
)

const (
	AppLabel          = "choerodon.io/application"
	AppVersionLabel   = "choerodon.io/version"
	ReleaseLabel      = "choerodon.io/release"
	NetworkLabel      = "choerodon.io/network"
	NetworkNoDelLabel = "choerodon.io/no_delete"
	AgentVersionLabel = "choerodon.io"
	CommitLabel       = "choerodon.io/commit"
	// 拼写错误，暂时不要更改
	CommandLabel        = "choeroodn.io/command"
	V1CommandLabel      = "choerodon.io/v1-command"
	AppServiceIdLabel   = "choerodon.io/app-service-id"
	V1AppServiceIdLabel = "choerodon.io/v1-app-service-id"
	TestLabel           = "choerodon.io/test"
)

func warning(format string, v ...interface{}) {
	format = fmt.Sprintf("WARNING: %s\n", format)
	fmt.Fprintf(os.Stderr, format, v...)
}

func AddLabel(info *resource.Info, clientSet *kubernetes.Clientset, c7nOptions *Install) error {
	t := info.Object.(*unstructured.Unstructured)
	kind := info.Mapping.GroupVersionKind.Kind

	l := t.GetLabels()

	if l == nil {
		l = make(map[string]string)
	}

	var addBaseLabels = func() {
		l[ReleaseLabel] = c7nOptions.ReleaseName
		l[AgentVersionLabel] = c7nOptions.C7NOptions.AgentVersion
		l[CommitLabel] = c7nOptions.C7NOptions.Commit
	}
	var addAppLabels = func() {
		l[AppLabel] = c7nOptions.C7NOptions.ChartName
		l[AppVersionLabel] = c7nOptions.Version
	}

	var addTemplateAppLabels = func() {
		tplLabels := getTemplateLabels(t.Object)
		tplLabels[ReleaseLabel] = c7nOptions.ReleaseName
		tplLabels[AgentVersionLabel] = c7nOptions.C7NOptions.AgentVersion
		tplLabels[CommitLabel] = c7nOptions.C7NOptions.Commit
		//12.05 新增打标签。
		//0 表示的是安装未填入值 -1代表更新
		if (c7nOptions.C7NOptions.AppServiceId != 0 && c7nOptions.C7NOptions.AppServiceId != -1) ||
			(c7nOptions.C7NOptions.V1AppServiceId != "0" && c7nOptions.C7NOptions.V1AppServiceId != "-1") {
			tplLabels[AppServiceIdLabel] = strconv.FormatInt(c7nOptions.C7NOptions.AppServiceId, 10)
			tplLabels[V1AppServiceIdLabel] = c7nOptions.C7NOptions.V1AppServiceId
		}
		if !c7nOptions.C7NOptions.IsTest {
			tplLabels[CommandLabel] = strconv.Itoa(int(c7nOptions.C7NOptions.Command))
			tplLabels[V1CommandLabel] = c7nOptions.C7NOptions.V1Command
		}
		tplLabels[AppLabel] = c7nOptions.C7NOptions.ChartName
		tplLabels[AppVersionLabel] = c7nOptions.Version
		if err := setTemplateLabels(t.Object, tplLabels); err != nil {
			warning("Set Template Labels failed, %v", err)
		}
	}
	var addSelectorAppLabels = func() {
		selectorLabels, _, err := unstructured.NestedStringMap(t.Object, "spec", "selector", "matchLabels")
		if err != nil {
			warning("Get Selector Labels failed, %v", err)
		}
		if selectorLabels == nil {
			selectorLabels = make(map[string]string)
		}
		selectorLabels[ReleaseLabel] = c7nOptions.ReleaseName
		if err := unstructured.SetNestedStringMap(t.Object, selectorLabels, "spec", "selector", "matchLabels"); err != nil {
			warning("Set Selector label failed, %v", err)
		}
	}

	// add private image pull secrets
	var addImagePullSecrets = func() {
		secrets, _, err := nestedLocalObjectReferences(t.Object, "spec", "template", "spec", "imagePullSecrets")
		if err != nil {
			warning("Get ImagePullSecrets failed, %v", err)
		}
		if secrets == nil {
			secrets = make([]v1.LocalObjectReference, 0)

		}
		secrets = append(secrets, c7nOptions.C7NOptions.ImagePullSecret...)
		// SetNestedField method just support a few types
		s := make([]interface{}, 0)
		for _, secret := range secrets {
			m := make(map[string]interface{})
			m["name"] = secret.Name
			s = append(s, m)
		}
		if err := unstructured.SetNestedField(t.Object, s, "spec", "template", "spec", "imagePullSecrets"); err != nil {
			warning("Set ImagePullSecrets failed, %v", err)
		}
	}

	switch kind {
	case "ReplicationController", "ReplicaSet", "Deployment":
		addAppLabels()
		addTemplateAppLabels()
		addSelectorAppLabels()
		addImagePullSecrets()
		if c7nOptions.IsUpgrade {
			if c7nOptions.C7NOptions.ReplicasStrategy == "replicas" {
				if kind == "ReplicaSet" {
					rs, err := clientSet.AppsV1().ReplicaSets(c7nOptions.Namespace).Get(context.Background(), t.GetName(), metav1.GetOptions{})
					if errors.IsNotFound(err) {
						break
					}
					if err != nil {
						warning("Failed to get ReplicaSet,error is %s.", err.Error())
						return err
					}
					err = setReplicas(t.Object, int64(*rs.Spec.Replicas))
					if err != nil {
						warning("Failed to set replicas,error is %s", err.Error())
						return err
					}
				}
				if kind == "Deployment" {
					dp, err := clientSet.AppsV1().Deployments(c7nOptions.Namespace).Get(context.Background(), t.GetName(), metav1.GetOptions{})
					if errors.IsNotFound(err) {
						break
					}
					if err != nil {
						warning("Failed to get ReplicaSet,error is %s.", err.Error())
						return err
					}
					err = setReplicas(t.Object, int64(*dp.Spec.Replicas))
					if err != nil {
						warning("Failed to set replicas,error is %s", err.Error())
						return err
					}
				}
			}
		}
	case "ConfigMap":
	case "Service":
		l[NetworkLabel] = "service"
		l[NetworkNoDelLabel] = "true"
	case "Ingress":
		l[NetworkLabel] = "ingress"
		l[NetworkNoDelLabel] = "true"
	case "Job":
		addImagePullSecrets()
		tplLabels := getTemplateLabels(t.Object)
		if c7nOptions.C7NOptions.IsTest {
			l[TestLabel] = c7nOptions.C7NOptions.TestLabel
			tplLabels[TestLabel] = c7nOptions.C7NOptions.TestLabel
			tplLabels[ReleaseLabel] = c7nOptions.ReleaseName
		}
		tplLabels[CommitLabel] = c7nOptions.C7NOptions.Commit
		if err := setTemplateLabels(t.Object, tplLabels); err != nil {
			warning("Set Test-Template Labels failed, %v", err)
		}
	case "DaemonSet", "StatefulSet":
		addAppLabels()
		addTemplateAppLabels()
		addImagePullSecrets()
		if c7nOptions.IsUpgrade {
			if kind == "StatefulSet" && c7nOptions.C7NOptions.ReplicasStrategy == "replicas" {
				sts, err := clientSet.AppsV1().StatefulSets(c7nOptions.Namespace).Get(context.Background(), t.GetName(), metav1.GetOptions{})
				if errors.IsNotFound(err) {
					break
				}
				if err != nil {
					warning("Failed to get ReplicaSet,error is %s.", err.Error())
					return err
				}
				err = setReplicas(t.Object, int64(*sts.Spec.Replicas))
				if err != nil {
					warning("Failed to set replicas,error is %s", err.Error())
					return err
				}
			}
		}
	case "Secret":
		addAppLabels()
	case "Pod":
		addAppLabels()
	case "PersistentVolumeClaim":
	default:
		warning("Skipping to add choerodon label, object: Kind %s of Release %s", kind, c7nOptions.ReleaseName)
		return nil
	}
	if t.GetNamespace() != "" && t.GetNamespace() != c7nOptions.Namespace && c7nOptions.C7NOptions.ChartName != "prometheus-operator" {
		return fmt.Errorf(" Kind:%s Name:%s. The namespace of this resource is not consistent with helm release", kind, t.GetName())
	}
	// add base labels
	addBaseLabels()
	t.SetLabels(l)

	annotations := t.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[CommitLabel] = c7nOptions.C7NOptions.Commit
	t.SetAnnotations(annotations)
	return nil
}

func setTemplateLabels(obj map[string]interface{}, templateLabels map[string]string) error {
	return unstructured.SetNestedStringMap(obj, templateLabels, "spec", "template", "metadata", "labels")
}

func setReplicas(obj map[string]interface{}, value int64) error {
	return unstructured.SetNestedField(obj, value, "spec", "replicas")
}

func getTemplateLabels(obj map[string]interface{}) map[string]string {
	tplLabels, _, err := unstructured.NestedStringMap(obj, "spec", "template", "metadata", "labels")
	if err != nil {
		warning("Get Template Labels failed, %v", err)
	}
	if tplLabels == nil {
		tplLabels = make(map[string]string)
	}
	return tplLabels
}

func nestedLocalObjectReferences(obj map[string]interface{}, fields ...string) ([]v1.LocalObjectReference, bool, error) {
	val, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found || err != nil {
		return nil, found, err
	}

	m, ok := val.([]v1.LocalObjectReference)
	if ok {
		return m, true, nil
		//return nil, false, fmt.Errorf("%v accessor error: %v is of the type %T, expected []v1.LocalObjectReference", strings.Join(fields, "."), val, val)
	}

	if m, ok := val.([]interface{}); ok {
		secrets := make([]v1.LocalObjectReference, 0)
		for _, v := range m {
			if vv, ok := v.(map[string]interface{}); ok {
				v2 := vv["name"]
				secret := v1.LocalObjectReference{}
				if secret.Name, ok = v2.(string); ok {
					secrets = append(secrets, secret)
				}
			}
		}
		return secrets, true, nil
	}
	return m, true, nil
}
