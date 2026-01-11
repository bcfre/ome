package inferenceservice

import (
	"context"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	knapis "knative.dev/pkg/apis"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/components"
	rbgpkg "github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/rbg"
)

// buildEngineComponentConfig 从Engine组件实例构建ComponentConfig
func buildEngineComponentConfig(
	ctx context.Context,
	engine *components.Engine,
	isvc *v1beta1.InferenceService,
	deploymentMode constants.DeploymentModeType,
) (*rbgpkg.ComponentConfig, error) {
	// 1. 调谐ObjectMeta
	objectMeta, err := engine.ReconcileObjectMeta(isvc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconcile engine object metadata")
	}

	// 2. 调谐PodSpec
	podSpec, err := engine.ReconcilePodSpec(isvc, &objectMeta)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconcile engine pod spec")
	}

	// 3. 获取ComponentExtensionSpec
	componentSpec := engine.GetComponentSpec()

	// 4. 构建ComponentConfig
	config := &rbgpkg.ComponentConfig{
		ComponentType:          v1beta1.EngineComponent,
		DeploymentMode:         deploymentMode,
		PodSpec:                podSpec,
		ComponentExtensionSpec: componentSpec,
		ObjectMeta:             objectMeta,
	}

	// 5. 如果是MultiNode模式，需要获取Leader和Worker PodSpec
	if deploymentMode == constants.MultiNode {
		workerPodSpec, err := engine.ReconcileWorkerPodSpec(isvc, &objectMeta)
		if err != nil {
			return nil, errors.Wrap(err, "failed to reconcile engine worker pod spec")
		}

		workerSize := engine.GetWorkerSize()

		config.LeaderPodSpec = podSpec // Leader使用默认PodSpec
		config.WorkerPodSpec = workerPodSpec
		config.WorkerSize = workerSize
	}

	return config, nil
}

// buildDecoderComponentConfig 从Decoder组件实例构建ComponentConfig
func buildDecoderComponentConfig(
	ctx context.Context,
	decoder *components.Decoder,
	isvc *v1beta1.InferenceService,
	deploymentMode constants.DeploymentModeType,
) (*rbgpkg.ComponentConfig, error) {
	// 1. 调谐ObjectMeta
	objectMeta, err := decoder.ReconcileObjectMeta(isvc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconcile decoder object metadata")
	}

	// 2. 调谐PodSpec
	podSpec, err := decoder.ReconcilePodSpec(isvc, &objectMeta)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconcile decoder pod spec")
	}

	// 3. 获取ComponentExtensionSpec
	componentSpec := decoder.GetComponentSpec()

	// 4. 构建ComponentConfig
	config := &rbgpkg.ComponentConfig{
		ComponentType:          v1beta1.DecoderComponent,
		DeploymentMode:         deploymentMode,
		PodSpec:                podSpec,
		ComponentExtensionSpec: componentSpec,
		ObjectMeta:             objectMeta,
	}

	// 5. 如果是MultiNode模式，需要获取Leader和Worker PodSpec
	if deploymentMode == constants.MultiNode {
		workerPodSpec, err := decoder.ReconcileWorkerPodSpec(isvc, &objectMeta)
		if err != nil {
			return nil, errors.Wrap(err, "failed to reconcile decoder worker pod spec")
		}

		workerSize := decoder.GetWorkerSize()

		config.LeaderPodSpec = podSpec // Leader使用默认PodSpec
		config.WorkerPodSpec = workerPodSpec
		config.WorkerSize = workerSize
	}

	return config, nil
}

// buildRouterComponentConfig 从Router组件实例构建ComponentConfig
func buildRouterComponentConfig(
	ctx context.Context,
	router *components.Router,
	isvc *v1beta1.InferenceService,
	deploymentMode constants.DeploymentModeType,
) (*rbgpkg.ComponentConfig, error) {
	// 1. 调谐ObjectMeta
	objectMeta, err := router.ReconcileObjectMeta(isvc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconcile router object metadata")
	}

	// 2. 调谐PodSpec
	podSpec, err := router.ReconcilePodSpec(isvc, &objectMeta)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconcile router pod spec")
	}

	// 3. 获取ComponentExtensionSpec
	componentSpec := router.GetComponentSpec()

	// 4. 构建ComponentConfig
	config := &rbgpkg.ComponentConfig{
		ComponentType:          v1beta1.RouterComponent,
		DeploymentMode:         deploymentMode,
		PodSpec:                podSpec,
		ComponentExtensionSpec: componentSpec,
		ObjectMeta:             objectMeta,
	}

	// Router目前不支持MultiNode模式，无需处理Worker配置

	return config, nil
}

// propagateRBGStatusToInferenceService 将RBG状态传播到InferenceService状态
func propagateRBGStatusToInferenceService(
	isvc *v1beta1.InferenceService,
	rbgStatus *rbgpkg.RBGStatus,
	componentList []v1beta1.ComponentType,
) {
	if rbgStatus == nil {
		return
	}

	// 初始化组件状态映射
	if isvc.Status.Components == nil {
		isvc.Status.Components = make(map[v1beta1.ComponentType]v1beta1.ComponentStatusSpec)
	}

	// 判断整体是否就绪
	allReady := true
	for _, componentType := range componentList {
		if roleStatus, exists := rbgStatus.RoleStatuses[componentType]; exists {
			// 检查Role是否就绪
			isReady := roleStatus.ReadyReplicas == roleStatus.Replicas && roleStatus.Replicas > 0
			conditionType := getComponentReadyCondition(componentType)

			if isReady {
				isvc.Status.SetCondition(conditionType, &knapis.Condition{
					Type:   conditionType,
					Status: v1.ConditionTrue,
					Reason: "RoleReady",
				})
			} else {
				allReady = false
				isvc.Status.SetCondition(conditionType, &knapis.Condition{
					Type:   conditionType,
					Status: v1.ConditionFalse,
					Reason: "RoleNotReady",
				})
			}
		} else {
			allReady = false
		}
	}

	// 设置InferenceService整体Ready条件
	if allReady {
		isvc.Status.SetCondition(knapis.ConditionReady, &knapis.Condition{
			Type:   knapis.ConditionReady,
			Status: v1.ConditionTrue,
			Reason: "AllComponentsReady",
		})
	} else {
		isvc.Status.SetCondition(knapis.ConditionReady, &knapis.Condition{
			Type:   knapis.ConditionReady,
			Status: v1.ConditionFalse,
			Reason: "ComponentsNotReady",
		})
	}
}

// getComponentReadyCondition 根据组件类型返回对应的Ready条件类型
func getComponentReadyCondition(componentType v1beta1.ComponentType) knapis.ConditionType {
	switch componentType {
	case v1beta1.EngineComponent:
		return v1beta1.EngineReady
	case v1beta1.DecoderComponent:
		return v1beta1.DecoderReady
	case v1beta1.RouterComponent:
		return v1beta1.RouterReady
	default:
		return knapis.ConditionReady
	}
}
