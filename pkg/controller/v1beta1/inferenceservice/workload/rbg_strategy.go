package workload

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/components"
	rbgpkg "github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/rbg"
)

// RBGStrategy 实现RBG (RoleBasedGroup) All-in-One工作负载策略
// 将多个组件（Engine、Decoder、Router）打包到一个RBG资源中
type RBGStrategy struct {
	client client.Client
	scheme *runtime.Scheme
	log    logr.Logger
}

// NewRBGStrategy 创建RBG策略实例
func NewRBGStrategy(client client.Client, scheme *runtime.Scheme, log logr.Logger) *RBGStrategy {
	return &RBGStrategy{
		client: client,
		scheme: scheme,
		log:    log,
	}
}

// GetStrategyName 返回策略名称
func (r *RBGStrategy) GetStrategyName() string {
	return "RBG"
}

// IsApplicable 判断该策略是否适用
// RBG策略必须通过annotation显式指定
func (r *RBGStrategy) IsApplicable(isvc *v1beta1.InferenceService, annotations map[string]string) bool {
	if mode, found := annotations[constants.DeploymentMode]; found {
		return mode == string(constants.RoleBasedGroup)
	}
	return false
}

// ValidateDeploymentModes 验证部署模式
// RBG策略仅支持RawDeployment和MultiNode
func (r *RBGStrategy) ValidateDeploymentModes(modes *ComponentDeploymentModes) error {
	// 检查Engine部署模式（如果存在）
	if modes.Engine != "" {
		if modes.Engine != constants.RawDeployment && modes.Engine != constants.MultiNode {
			return fmt.Errorf("RBG mode only supports RawDeployment and MultiNode deployment modes for engine, got %s", modes.Engine)
		}
	}

	// 检查Decoder部署模式（如果存在）
	if modes.Decoder != "" {
		if modes.Decoder != constants.RawDeployment && modes.Decoder != constants.MultiNode {
			return fmt.Errorf("RBG mode only supports RawDeployment and MultiNode deployment modes for decoder, got %s", modes.Decoder)
		}
	}

	// 检查Router部署模式（如果存在）
	if modes.Router != "" {
		if modes.Router != constants.RawDeployment {
			return fmt.Errorf("RBG mode only supports RawDeployment deployment mode for router, got %s", modes.Router)
		}
	}

	return nil
}

// ReconcileWorkload 执行工作负载调谐
// 使用ComponentConfigExtractor接口从组件提取配置，然后调用RBGReconciler
func (r *RBGStrategy) ReconcileWorkload(ctx context.Context, request *WorkloadReconcileRequest) (*ReconcileResult, error) {
	r.log.Info("Reconciling with RBG strategy",
		"namespace", request.InferenceService.Namespace,
		"inferenceService", request.InferenceService.Name)

	// 收集ComponentConfig
	componentConfigs := make(map[v1beta1.ComponentType]*rbgpkg.ComponentConfig)

	// 构建Engine ComponentConfig
	if request.MergedEngine != nil {
		r.log.Info("Building engine component config for RBG",
			"deploymentMode", request.DeploymentModes.Engine)

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

		// 类型断言为Engine组件并提取配置
		engineComponent, ok := engineReconciler.(components.ComponentConfigExtractor)
		if !ok {
			return nil, fmt.Errorf("engine component does not implement ComponentConfigExtractor")
		}

		engineConfig, err := r.buildComponentConfig(ctx, engineComponent, request.InferenceService, v1beta1.EngineComponent, request.DeploymentModes.Engine)
		if err != nil {
			return nil, fmt.Errorf("failed to build engine config: %w", err)
		}
		componentConfigs[v1beta1.EngineComponent] = engineConfig
	}

	// 构建Decoder ComponentConfig
	if request.MergedDecoder != nil {
		r.log.Info("Building decoder component config for RBG",
			"deploymentMode", request.DeploymentModes.Decoder)

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

		// 类型断言为Decoder组件并提取配置
		decoderComponent, ok := decoderReconciler.(components.ComponentConfigExtractor)
		if !ok {
			return nil, fmt.Errorf("decoder component does not implement ComponentConfigExtractor")
		}

		decoderConfig, err := r.buildComponentConfig(ctx, decoderComponent, request.InferenceService, v1beta1.DecoderComponent, request.DeploymentModes.Decoder)
		if err != nil {
			return nil, fmt.Errorf("failed to build decoder config: %w", err)
		}
		componentConfigs[v1beta1.DecoderComponent] = decoderConfig
	}

	// 构建Router ComponentConfig
	if request.MergedRouter != nil {
		r.log.Info("Building router component config for RBG",
			"deploymentMode", request.DeploymentModes.Router)

		routerReconciler := request.ComponentBuilderFactory.CreateRouterComponent(
			request.DeploymentModes.Router,
			request.BaseModel,
			request.BaseModelMeta,
			request.MergedRouter,
			request.Runtime,
			request.RuntimeName,
		)

		// 类型断言为Router组件并提取配置
		routerComponent, ok := routerReconciler.(components.ComponentConfigExtractor)
		if !ok {
			return nil, fmt.Errorf("router component does not implement ComponentConfigExtractor")
		}

		routerConfig, err := r.buildComponentConfig(ctx, routerComponent, request.InferenceService, v1beta1.RouterComponent, request.DeploymentModes.Router)
		if err != nil {
			return nil, fmt.Errorf("failed to build router config: %w", err)
		}
		componentConfigs[v1beta1.RouterComponent] = routerConfig
	}

	// 创建RBG Reconciler
	rbgReconciler := rbgpkg.NewRBGReconciler(r.client, r.scheme)

	// 调谐RBG资源
	rbgResult, err := rbgReconciler.Reconcile(ctx, request.InferenceService, componentConfigs)
	if err != nil {
		r.log.Error(err, "Failed to reconcile RBG")
		return nil, fmt.Errorf("failed to reconcile RBG: %w", err)
	}

	// 获取RBG状态
	rbgStatus, err := rbgReconciler.GetStatus(ctx, request.InferenceService.Namespace, request.InferenceService.Name)
	if err != nil {
		r.log.Error(err, "Failed to get RBG status")
		return nil, fmt.Errorf("failed to get RBG status: %w", err)
	}

	// 构建component列表
	var componentList []v1beta1.ComponentType
	if request.MergedEngine != nil {
		componentList = append(componentList, v1beta1.EngineComponent)
	}
	if request.MergedDecoder != nil {
		componentList = append(componentList, v1beta1.DecoderComponent)
	}
	if request.MergedRouter != nil {
		componentList = append(componentList, v1beta1.RouterComponent)
	}

	// 状态传播将在PropagateStatus方法中完成
	// 这里创建一个RBGStatus包装
	status := &RBGStatus{
		rbgStatus:      rbgStatus,
		componentList:  componentList,
		inferenceService: request.InferenceService,
	}

	// 确定Ingress部署模式
	ingressDeploymentMode := r.GetIngressDeploymentMode(request.DeploymentModes)

	r.log.Info("RBG strategy reconciliation completed",
		"namespace", request.InferenceService.Namespace,
		"inferenceService", request.InferenceService.Name,
		"rbgReady", rbgResult.Ready,
		"ingressDeploymentMode", ingressDeploymentMode)

	return &ReconcileResult{
		Result:                ctrl.Result{},
		WorkloadStatus:        status,
		IngressDeploymentMode: ingressDeploymentMode,
	}, nil
}

// buildComponentConfig 从组件提取配置构建RBG ComponentConfig
func (r *RBGStrategy) buildComponentConfig(
	ctx context.Context,
	component components.ComponentConfigExtractor,
	isvc *v1beta1.InferenceService,
	componentType v1beta1.ComponentType,
	deploymentMode constants.DeploymentModeType,
) (*rbgpkg.ComponentConfig, error) {
	// 提取ObjectMeta
	objectMeta, err := component.GetObjectMeta(isvc)
	if err != nil {
		return nil, fmt.Errorf("failed to get object meta: %w", err)
	}

	// 提取PodSpec
	podSpec, err := component.GetPodSpec(isvc)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod spec: %w", err)
	}

	// 提取WorkerPodSpec（如果支持）
	workerPodSpec, err := component.GetWorkerPodSpec(isvc)
	if err != nil {
		return nil, fmt.Errorf("failed to get worker pod spec: %w", err)
	}

	// 提取ComponentExtension
	componentExt := component.GetComponentExtension()

	// 提取WorkerSize
	workerSize := component.GetWorkerSize()

	return &rbgpkg.ComponentConfig{
		ComponentType:          componentType,
		DeploymentMode:         deploymentMode,
		PodSpec:                podSpec,
		LeaderPodSpec:          podSpec, // For MultiNode, leader uses the main PodSpec
		WorkerPodSpec:          workerPodSpec,
		WorkerSize:             workerSize,
		ComponentExtensionSpec: componentExt,
		ObjectMeta:             objectMeta,
	}, nil
}

// GetIngressDeploymentMode 确定Ingress部署模式
func (r *RBGStrategy) GetIngressDeploymentMode(modes *ComponentDeploymentModes) constants.DeploymentModeType {
	// 优先级：Router > Decoder > Engine
	if modes.Router != "" {
		return modes.Router
	}
	if modes.Decoder != "" {
		return modes.Decoder
	}
	return modes.Engine
}

// PropagateStatus 状态传播到InferenceService
// 复用现有的propagateRBGStatusToInferenceService逻辑
func (r *RBGStrategy) PropagateStatus(isvc *v1beta1.InferenceService, workloadStatus WorkloadStatus, components []v1beta1.ComponentType) error {
	// 类型断言为RBGStatus
	rbgStatus, ok := workloadStatus.(*RBGStatus)
	if !ok {
		return fmt.Errorf("workload status is not RBGStatus type")
	}

	// 调用现有的状态传播函数
	propagateRBGStatusToInferenceService(isvc, rbgStatus.rbgStatus, components)

	r.log.V(1).Info("RBG status propagated to InferenceService",
		"namespace", isvc.Namespace,
		"inferenceService", isvc.Name)

	return nil
}

// RBGStatus 实现WorkloadStatus接口，封装RBG状态
type RBGStatus struct {
	rbgStatus        *rbgpkg.RBGStatus
	componentList    []v1beta1.ComponentType
	inferenceService *v1beta1.InferenceService
}

// GetComponentStatus 获取指定组件的状态
func (s *RBGStatus) GetComponentStatus(componentType v1beta1.ComponentType) *v1beta1.ComponentStatusSpec {
	if s.rbgStatus == nil || s.rbgStatus.RoleStatuses == nil {
		return nil
	}
	
	// ComponentStatusSpec主要包含URL和Revision信息
	// RBG状态中的副本数信息不直接映射到ComponentStatusSpec
	return &v1beta1.ComponentStatusSpec{
		// RBG场景下，URL等信息由Ingress层负责设置
	}
}

// IsReady 判断整体是否就绪
func (s *RBGStatus) IsReady() bool {
	if s.rbgStatus == nil || s.rbgStatus.RoleStatuses == nil {
		return false
	}
	
	// 所有Role都Ready才算整体Ready
	for _, componentType := range s.componentList {
		if roleStatus, ok := s.rbgStatus.RoleStatuses[componentType]; ok {
			if roleStatus.ReadyReplicas < roleStatus.Replicas {
				return false
			}
		}
	}
	return true
}

// GetConditions 获取状态条件
func (s *RBGStatus) GetConditions() []interface{} {
	if s.rbgStatus == nil {
		return []interface{}{}
	}
	// 将Conditions转换为interface切片
	conditions := make([]interface{}, len(s.rbgStatus.Conditions))
	for i, cond := range s.rbgStatus.Conditions {
		conditions[i] = cond
	}
	return conditions
}

// propagateRBGStatusToInferenceService 是从rbg_helper.go中导入的函数
// 这里需要确保能够访问到该函数，可能需要将其移到一个公共包或者在这里重新实现
// 为了保持代码复用，我们假设可以直接调用
func propagateRBGStatusToInferenceService(isvc *v1beta1.InferenceService, rbgStatus *rbgpkg.RBGStatus, componentList []v1beta1.ComponentType) {
	// 这个函数的实现在 pkg/controller/v1beta1/inferenceservice/rbg_helper.go
	// 由于包结构限制，我们在这里简单实现状态传播逻辑
	
	if isvc.Status.Components == nil {
		isvc.Status.Components = make(map[v1beta1.ComponentType]v1beta1.ComponentStatusSpec)
	}

	// 从RBG RoleStatuses映射到InferenceService ComponentStatuses
	// ComponentStatusSpec主要用于URL和Revision信息，不包含副本数
	for _, componentType := range componentList {
		if _, ok := rbgStatus.RoleStatuses[componentType]; ok {
			// 初始化ComponentStatusSpec，实际URL和状态由Ingress和其他协调器设置
			if _, exists := isvc.Status.Components[componentType]; !exists {
				isvc.Status.Components[componentType] = v1beta1.ComponentStatusSpec{}
			}
		}
	}
}
