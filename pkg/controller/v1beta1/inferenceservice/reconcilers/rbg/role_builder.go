package rbg

import (
	"encoding/json"

	workloadsv1alpha1 "github.com/bcfre/rbg-api/api/workloads/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
)

// buildRoleSpec根据ComponentConfig构建RoleSpec
func buildRoleSpec(config *ComponentConfig) (*workloadsv1alpha1.RoleSpec, error) {
	roleName := getRoleName(config.ComponentType)

	// 获取副本数
	replicas := getReplicasCount(config.ComponentExtensionSpec)

	// 构建Role基础配置
	role := &workloadsv1alpha1.RoleSpec{
		Name:        roleName,
		Labels:      buildRoleLabels(config),
		Annotations: buildRoleAnnotations(config),
		Replicas:    replicas,
	}

	// 根据部署模式配置Workload和Template
	switch config.DeploymentMode {
	case constants.RawDeployment:
		role.Workload = workloadsv1alpha1.WorkloadSpec{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		}
		role.Template = buildPodTemplateSpec(config)

	case constants.MultiNode:
		role.Workload = workloadsv1alpha1.WorkloadSpec{
			APIVersion: "leaderworkerset.x-k8s.io/v1",
			Kind:       "LeaderWorkerSet",
		}
		// 构建LeaderWorkerSet配置
		role.LeaderWorkerSet = buildLeaderWorkerTemplate(config)

	default:
		// 默认使用StatefulSet
		role.Workload = workloadsv1alpha1.WorkloadSpec{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		}
		role.Template = buildPodTemplateSpec(config)
	}

	return role, nil
}

// getRoleName根据ComponentType生成Role名称
func getRoleName(componentType v1beta1.ComponentType) string {
	switch componentType {
	case v1beta1.EngineComponent:
		return "engine"
	case v1beta1.DecoderComponent:
		return "decoder"
	case v1beta1.RouterComponent:
		return "router"
	default:
		return string(componentType)
	}
}

// getReplicasCount获取副本数
func getReplicasCount(spec *v1beta1.ComponentExtensionSpec) *int32 {
	if spec == nil || spec.MinReplicas == nil {
		defaultReplicas := int32(1)
		return &defaultReplicas
	}
	replicas := int32(*spec.MinReplicas)
	return &replicas
}

// buildRoleLabels构建Role的标签
func buildRoleLabels(config *ComponentConfig) map[string]string {
	labels := make(map[string]string)

	// 复制ObjectMeta中的标签
	if config.ObjectMeta.Labels != nil {
		for k, v := range config.ObjectMeta.Labels {
			labels[k] = v
		}
	}

	// 复制ComponentExtensionSpec中的标签
	if config.ComponentExtensionSpec != nil && config.ComponentExtensionSpec.Labels != nil {
		for k, v := range config.ComponentExtensionSpec.Labels {
			labels[k] = v
		}
	}

	// 添加component标识标签
	labels["component"] = getRoleName(config.ComponentType)

	return labels
}

// buildRoleAnnotations构建Role的注解
func buildRoleAnnotations(config *ComponentConfig) map[string]string {
	annotations := make(map[string]string)

	// 复制ObjectMeta中的注解
	if config.ObjectMeta.Annotations != nil {
		for k, v := range config.ObjectMeta.Annotations {
			annotations[k] = v
		}
	}

	// 复制ComponentExtensionSpec中的注解
	if config.ComponentExtensionSpec != nil && config.ComponentExtensionSpec.Annotations != nil {
		for k, v := range config.ComponentExtensionSpec.Annotations {
			annotations[k] = v
		}
	}

	return annotations
}

// buildPodTemplateSpec构建PodTemplateSpec
func buildPodTemplateSpec(config *ComponentConfig) *corev1.PodTemplateSpec {
	if config.PodSpec == nil {
		return nil
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      buildRoleLabels(config),
			Annotations: buildRoleAnnotations(config),
		},
		Spec: *config.PodSpec,
	}
}

// buildLeaderWorkerTemplate构建LeaderWorkerTemplate配置
func buildLeaderWorkerTemplate(config *ComponentConfig) *workloadsv1alpha1.LeaderWorkerTemplate {
	if config.WorkerSize <= 0 {
		// 如果没有指定worker数量，默认使用1个leader + 0个worker
		config.WorkerSize = 0
	}

	// size = 1个leader + N个worker
	size := int32(config.WorkerSize + 1)

	lwt := &workloadsv1alpha1.LeaderWorkerTemplate{
		Size: &size,
	}

	// 构建Leader PodTemplateSpec
	if config.LeaderPodSpec != nil {
		leaderTemplate := &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      buildRoleLabels(config),
				Annotations: buildRoleAnnotations(config),
			},
			Spec: *config.LeaderPodSpec,
		}
		// 转换为RawExtension
		leaderJSON, err := json.Marshal(leaderTemplate)
		if err == nil {
			lwt.PatchLeaderTemplate = &runtime.RawExtension{
				Raw: leaderJSON,
			}
		}
	}

	// 构建Worker PodTemplateSpec
	if config.WorkerPodSpec != nil {
		workerTemplate := &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      buildRoleLabels(config),
				Annotations: buildRoleAnnotations(config),
			},
			Spec: *config.WorkerPodSpec,
		}
		// 转换为RawExtension
		workerJSON, err := json.Marshal(workerTemplate)
		if err == nil {
			lwt.PatchWorkerTemplate = &runtime.RawExtension{
				Raw: workerJSON,
			}
		}
	}

	return lwt
}
