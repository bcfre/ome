package workload

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	EngineAcceleratorClass      *v1beta1.AcceleratorClassSpec
	EngineAcceleratorClassName  string
	DecoderAcceleratorClass     *v1beta1.AcceleratorClassSpec
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
