package workload

import (
	"context"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
)

// WorkloadStrategy 定义工作负载策略的统一接口
// 所有工作负载策略（单组件、RBG、JobSet等）都需要实现此接口
type WorkloadStrategy interface {
	// GetStrategyName 返回策略名称
	GetStrategyName() string

	// IsApplicable 判断该策略是否适用于当前InferenceService
	// 通过检查annotations和其他条件来决定
	IsApplicable(isvc *v1beta1.InferenceService, annotations map[string]string) bool

	// ValidateDeploymentModes 验证部署模式是否被该策略支持
	// 不同策略支持的部署模式可能不同
	ValidateDeploymentModes(modes *ComponentDeploymentModes) error

	// ReconcileWorkload 执行工作负载调谐
	// 这是策略的核心方法，负责创建和更新工作负载资源
	ReconcileWorkload(ctx context.Context, request *WorkloadReconcileRequest) (*ReconcileResult, error)

	// GetIngressDeploymentMode 确定Ingress部署模式
	// 根据各组件的部署模式确定应该使用哪种部署模式进行Ingress配置
	GetIngressDeploymentMode(modes *ComponentDeploymentModes) constants.DeploymentModeType

	// PropagateStatus 状态传播到InferenceService
	// 将工作负载的状态信息传播到InferenceService的Status字段
	PropagateStatus(isvc *v1beta1.InferenceService, workloadStatus WorkloadStatus, components []v1beta1.ComponentType) error
}

// WorkloadStatus 定义工作负载状态的统一接口
// 用于抽象不同工作负载类型的状态信息
type WorkloadStatus interface {
	// GetComponentStatus 获取指定组件的状态
	GetComponentStatus(componentType v1beta1.ComponentType) *v1beta1.ComponentStatusSpec

	// IsReady 判断整体是否就绪
	IsReady() bool

	// GetConditions 获取状态条件
	GetConditions() []interface{}
}
