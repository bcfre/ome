package workload

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/components"
)

// WorkloadReconcileRequest 封装工作负载调谐请求参数
// 包含执行工作负载调谐所需的所有信息
type WorkloadReconcileRequest struct {
	// InferenceService实例
	InferenceService *v1beta1.InferenceService

	// 基础模型信息
	BaseModel     *v1beta1.BaseModelSpec
	BaseModelMeta *metav1.ObjectMeta

	// 运行时信息
	Runtime     *v1beta1.ServingRuntimeSpec
	RuntimeName string

	// 合并后的组件规格
	MergedEngine  *v1beta1.EngineSpec
	MergedDecoder *v1beta1.DecoderSpec
	MergedRouter  *v1beta1.RouterSpec

	// 部署模式配置
	DeploymentModes *ComponentDeploymentModes

	// 组件构建工厂
	ComponentBuilderFactory *components.ComponentBuilderFactory

	// 是否用户指定运行时
	UserSpecifiedRuntime bool

	// AcceleratorClass信息（如果需要）
	EngineAcceleratorClass     *v1beta1.AcceleratorClassSpec
	EngineAcceleratorClassName string
	DecoderAcceleratorClass    *v1beta1.AcceleratorClassSpec
	DecoderAcceleratorClassName string

	// SupportedModelFormat信息（如果需要）
	EngineSupportedModelFormat  *v1beta1.SupportedModelFormat
	DecoderSupportedModelFormat *v1beta1.SupportedModelFormat
}

// ComponentDeploymentModes 封装各组件的部署模式
type ComponentDeploymentModes struct {
	Engine  constants.DeploymentModeType
	Decoder constants.DeploymentModeType
	Router  constants.DeploymentModeType
}

// ReconcileResult 统一的调谐结果返回结构
type ReconcileResult struct {
	// Kubernetes标准调谐结果
	Result ctrl.Result

	// 工作负载状态
	WorkloadStatus WorkloadStatus

	// 确定的Ingress部署模式
	IngressDeploymentMode constants.DeploymentModeType

	// 各组件的状态映射
	ComponentStatuses map[v1beta1.ComponentType]v1beta1.ComponentStatusSpec
}

// SingleComponentStatus 实现WorkloadStatus接口，用于单组件策略
type SingleComponentStatus struct {
	componentStatuses map[v1beta1.ComponentType]v1beta1.ComponentStatusSpec
	ready             bool
}

// NewSingleComponentStatus 创建SingleComponentStatus实例
func NewSingleComponentStatus() *SingleComponentStatus {
	return &SingleComponentStatus{
		componentStatuses: make(map[v1beta1.ComponentType]v1beta1.ComponentStatusSpec),
		ready:             false,
	}
}

// GetComponentStatus 获取指定组件的状态
func (s *SingleComponentStatus) GetComponentStatus(componentType v1beta1.ComponentType) *v1beta1.ComponentStatusSpec {
	if status, ok := s.componentStatuses[componentType]; ok {
		return &status
	}
	return nil
}

// IsReady 判断整体是否就绪
func (s *SingleComponentStatus) IsReady() bool {
	return s.ready
}

// GetConditions 获取状态条件
func (s *SingleComponentStatus) GetConditions() []interface{} {
	// 单组件策略的条件从各组件状态中聚合
	return []interface{}{}
}

// SetComponentStatus 设置组件状态
func (s *SingleComponentStatus) SetComponentStatus(componentType v1beta1.ComponentType, status v1beta1.ComponentStatusSpec) {
	s.componentStatuses[componentType] = status
}

// SetReady 设置就绪状态
func (s *SingleComponentStatus) SetReady(ready bool) {
	s.ready = ready
}

// GetAllComponentStatuses 获取所有组件状态
func (s *SingleComponentStatus) GetAllComponentStatuses() map[v1beta1.ComponentType]v1beta1.ComponentStatusSpec {
	return s.componentStatuses
}
