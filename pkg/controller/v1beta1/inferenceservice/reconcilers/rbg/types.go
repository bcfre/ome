package rbg

import (
	workloadsv1alpha1 "github.com/bcfre/rbg-api/api/workloads/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	knapis "knative.dev/pkg/apis"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
)

var _ = &workloadsv1alpha1.RoleBasedGroup{}

// ComponentConfig包含单个Component的所有配置
type ComponentConfig struct {
	// ComponentType指定组件类型(Engine/Decoder/Router)
	ComponentType v1beta1.ComponentType
	// DeploymentMode指定该组件的部署模式(RawDeployment或MultiNode)
	DeploymentMode constants.DeploymentModeType
	// PodSpec用于RawDeployment模式或作为MultiNode模式的基础模板
	PodSpec *corev1.PodSpec
	// LeaderPodSpec用于MultiNode模式的leader节点
	LeaderPodSpec *corev1.PodSpec
	// WorkerPodSpec用于MultiNode模式的worker节点
	WorkerPodSpec *corev1.PodSpec
	// WorkerSize指定worker节点数量(MultiNode模式)
	WorkerSize int
	// ComponentExtensionSpec包含副本数、扩缩容等配置
	ComponentExtensionSpec *v1beta1.ComponentExtensionSpec
	// ObjectMeta包含名称、标签、注解等元数据
	ObjectMeta metav1.ObjectMeta
}

// RBGResult包含RBG协调的结果
type RBGResult struct {
	// RBG是创建/更新后的RoleBasedGroup资源
	RBG *workloadsv1alpha1.RoleBasedGroup
	// Ready表示RBG是否就绪
	Ready bool
	// URL是服务访问地址
	URL *knapis.URL
}

// RBGStatus包含RBG的状态信息
type RBGStatus struct {
	// RoleStatuses包含每个Role的状态，key为ComponentType
	RoleStatuses map[v1beta1.ComponentType]*RoleStatus
	// Conditions包含RBG的整体条件
	Conditions []metav1.Condition
}

// RoleStatus包含单个Role的状态
type RoleStatus struct {
	// ReadyReplicas是就绪的副本数
	ReadyReplicas int32
	// Replicas是期望的总副本数
	Replicas int32
	// UpdatedReplicas是已更新的副本数
	UpdatedReplicas int32
}
