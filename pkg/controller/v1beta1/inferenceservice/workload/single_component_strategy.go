package workload

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/components"
)

// SingleComponentStrategy 实现单组件独立部署策略
// 这是默认策略，每个组件独立调谐，不使用All-in-One工作负载
type SingleComponentStrategy struct {
	log logr.Logger
}

// NewSingleComponentStrategy 创建单组件策略实例
func NewSingleComponentStrategy(log logr.Logger) *SingleComponentStrategy {
	return &SingleComponentStrategy{
		log: log,
	}
}

// GetStrategyName 返回策略名称
func (s *SingleComponentStrategy) GetStrategyName() string {
	return "SingleComponent"
}

// IsApplicable 判断该策略是否适用
// 单组件策略是默认策略，当没有指定其他All-in-One工作负载时使用
func (s *SingleComponentStrategy) IsApplicable(isvc *v1beta1.InferenceService, annotations map[string]string) bool {
	// 检查是否指定了RoleBasedGroup模式
	if mode, found := annotations[constants.DeploymentMode]; found {
		if mode == string(constants.RoleBasedGroup) {
			return false
		}
	}

	// 默认情况下，单组件策略总是适用
	return true
}

// ValidateDeploymentModes 验证部署模式
// 单组件策略支持所有部署模式
func (s *SingleComponentStrategy) ValidateDeploymentModes(modes *ComponentDeploymentModes) error {
	// 单组件策略支持所有部署模式，无需验证
	return nil
}

// ReconcileWorkload 执行工作负载调谐
// 复用现有的组件Reconcile逻辑
func (s *SingleComponentStrategy) ReconcileWorkload(ctx context.Context, request *WorkloadReconcileRequest) (ctrl.Result, error) {
	s.log.Info("Reconciling with SingleComponent strategy",
		"namespace", request.InferenceService.Namespace,
		"inferenceService", request.InferenceService.Name)

	var reconcilers []components.Component

	// 创建Engine组件
	if request.MergedEngine != nil {
		s.log.Info("Creating engine reconciler",
			"deploymentMode", request.DeploymentModes.Engine,
			"namespace", request.InferenceService.Namespace,
			"inferenceService", request.InferenceService.Name)

		engineReconciler := request.ComponentBuilderFactory.CreateEngineComponent(
			request.DeploymentModes.Engine,
			request.BaseModel,
			request.BaseModelMeta,
			request.MergedEngine,
			request.Runtime,
			request.RuntimeName,
			request.EngineSupportedModelFormat,
			request.EngineAcceleratorClass,
			request.EngineAcceleratorClassName,
		)
		reconcilers = append(reconcilers, engineReconciler)
	}

	// 创建Decoder组件
	if request.MergedDecoder != nil {
		s.log.Info("Creating decoder reconciler",
			"deploymentMode", request.DeploymentModes.Decoder,
			"namespace", request.InferenceService.Namespace,
			"inferenceService", request.InferenceService.Name)

		decoderReconciler := request.ComponentBuilderFactory.CreateDecoderComponent(
			request.DeploymentModes.Decoder,
			request.BaseModel,
			request.BaseModelMeta,
			request.MergedDecoder,
			request.Runtime,
			request.RuntimeName,
			request.DecoderSupportedModelFormat,
			request.DecoderAcceleratorClass,
			request.DecoderAcceleratorClassName,
		)
		reconcilers = append(reconcilers, decoderReconciler)
	}

	// 创建Router组件
	if request.MergedRouter != nil {
		s.log.Info("Creating router reconciler",
			"deploymentMode", request.DeploymentModes.Router,
			"namespace", request.InferenceService.Namespace,
			"inferenceService", request.InferenceService.Name)

		routerReconciler := request.ComponentBuilderFactory.CreateRouterComponent(
			request.DeploymentModes.Router,
			request.BaseModel,
			request.BaseModelMeta,
			request.MergedRouter,
			request.Runtime,
			request.RuntimeName,
		)
		reconcilers = append(reconcilers, routerReconciler)
	}

	// 运行所有reconcilers
	for _, reconciler := range reconcilers {
		result, err := reconciler.Reconcile(request.InferenceService)
		if err != nil {
			s.log.Error(err, "Failed to reconcile component",
				"component", fmt.Sprintf("%T", reconciler),
				"namespace", request.InferenceService.Namespace,
				"inferenceService", request.InferenceService.Name)
			return result, err
		}
		if result.Requeue || result.RequeueAfter > 0 {
			return result, nil
		}
	}

	s.log.Info("SingleComponent strategy reconciliation completed",
		"namespace", request.InferenceService.Namespace,
		"inferenceService", request.InferenceService.Name)

	return reconcile.Result{}, nil
}
