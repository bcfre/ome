package workload

import (
	"context"
	"fmt"

	rbgapi "github.com/bcfre/rbg-api/api/workloads/v1alpha1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	knapis "knative.dev/pkg/apis"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/controllerconfig"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/components"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/autoscaler"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/ingress/services"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/pdb"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/rbac"
	rbgpkg "github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/reconcilers/rbg"
	"github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/status"
	isvcutils "github.com/sgl-project/ome/pkg/controller/v1beta1/inferenceservice/utils"
)

// RBGStrategy 实现RBG (RoleBasedGroup) All-in-One工作负载策略
// 将多个组件（Engine、Decoder、Router）打包到一个RBG资源中
type RBGStrategy struct {
	client    client.Client
	clientset kubernetes.Interface
	scheme    *runtime.Scheme
	log       logr.Logger
}

// NewRBGStrategy 创建RBG策略实例
func NewRBGStrategy(client client.Client, clientset kubernetes.Interface, scheme *runtime.Scheme, log logr.Logger) *RBGStrategy {
	return &RBGStrategy{
		client:    client,
		clientset: clientset,
		scheme:    scheme,
		log:       log,
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
func (r *RBGStrategy) ReconcileWorkload(ctx context.Context, request *WorkloadReconcileRequest) (ctrl.Result, error) {
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
			return ctrl.Result{}, fmt.Errorf("engine component does not implement ComponentConfigExtractor")
		}

		engineConfig, err := r.buildComponentConfig(ctx, engineComponent, request.InferenceService, v1beta1.EngineComponent, request.DeploymentModes.Engine)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to build engine config: %w", err)
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
			return ctrl.Result{}, fmt.Errorf("decoder component does not implement ComponentConfigExtractor")
		}

		decoderConfig, err := r.buildComponentConfig(ctx, decoderComponent, request.InferenceService, v1beta1.DecoderComponent, request.DeploymentModes.Decoder)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to build decoder config: %w", err)
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
			return ctrl.Result{}, fmt.Errorf("router component does not implement ComponentConfigExtractor")
		}

		routerConfig, err := r.buildComponentConfig(ctx, routerComponent, request.InferenceService, v1beta1.RouterComponent, request.DeploymentModes.Router)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to build router config: %w", err)
		}
		componentConfigs[v1beta1.RouterComponent] = routerConfig
	}

	// 阶段1: 创建RBAC资源（仅Router需要）
	if err := r.reconcileRBAC(ctx, request.InferenceService, componentConfigs); err != nil {
		r.log.Error(err, "Failed to reconcile RBAC resources")
		return ctrl.Result{}, fmt.Errorf("failed to reconcile RBAC: %w", err)
	}

	// 阶段2: 创建RBG Reconciler
	rbgReconciler := rbgpkg.NewRBGReconciler(r.client, r.scheme)

	// 调谐RBG资源
	result, err := rbgReconciler.Reconcile(ctx, request.InferenceService, componentConfigs)
	if err != nil {
		r.log.Error(err, "Failed to reconcile RBG")
		return result, fmt.Errorf("failed to reconcile RBG: %w", err)
	}
	if result.Requeue || result.RequeueAfter > 0 {
		return result, nil
	}

	// 阶段3: 创建HPA资源（为每个Role创建独立HPA）
	if err := r.reconcileHPA(ctx, request.InferenceService, componentConfigs); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile HPA: %w", err)
	}

	// 阶段4: 创建PDB资源（为每个Role创建独立PDB）
	if err := r.reconcilePDB(ctx, request.InferenceService, componentConfigs); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile PDB: %w", err)
	}

	if err := r.propagateStatus(request.InferenceService, componentConfigs); err != nil {
		r.log.Error(err, "Failed to propagate status, but continuing",
			"namespace", request.InferenceService.Namespace,
			"inferenceService", request.InferenceService.Name)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile Status: %w", err)
	}

	return ctrl.Result{}, nil
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

// propagateStatus 状态传播到InferenceService
// 从RBG资源读取状态并传播到各组件状态和条件
func (r *RBGStrategy) propagateStatus(isvc *v1beta1.InferenceService, componentConfig map[v1beta1.ComponentType]*rbgpkg.ComponentConfig) error {
	ctx := context.TODO()

	// 创建StatusReconciler
	statusReconciler := status.NewStatusReconciler()

	// 读取RBG资源
	rbg := &rbgapi.RoleBasedGroup{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      isvc.Name,
		Namespace: isvc.Namespace,
	}, rbg)
	if err != nil {
		return fmt.Errorf("failed to get RBG status: %w", err)
	}

	// 2. 遍历每个组件，传播组件状态和条件
	componentList := make([]v1beta1.ComponentType, 0, len(componentConfig))
	for componentType, config := range componentConfig {
		componentList = append(componentList, componentType)

		// 从cRBG状态中查找对应的Role状态
		var roleStatus *rbgapi.RoleStatus
		for i := range rbg.Status.RoleStatuses {
			if rbg.Status.RoleStatuses[i].Name == string(componentType) {
				roleStatus = &rbg.Status.RoleStatuses[i]
				break
			}
		}

		if roleStatus == nil {
			r.log.V(1).Info("No role status found for component, just initializing",
				"componentType", componentType)
			statusReconciler.InitializeComponentCondition(&isvc.Status, componentType)
			continue
		}

		// 构造URL
		url, err := createRawURL(r.clientset, config.ObjectMeta)
		if err != nil {
			return err
		}

		roleReady := false
		if roleStatus.Replicas == roleStatus.ReadyReplicas && roleStatus.Replicas == roleStatus.UpdatedReplicas {
			roleReady = true
		}

		// 调用StatusReconciler传播组件状态
		statusReconciler.PropagateRBGRoleStatus(
			&isvc.Status,
			componentType,
			roleReady,
			rbg,
			url,
		)

		r.log.V(1).Info("Propagated RBG status for component",
			"componentType", componentType,
			"replicas", roleStatus.Replicas,
			"readyReplicas", roleStatus.ReadyReplicas,
			"url", url.String())
	}

	// 3. 遍历每个组件，更新模型状态
	for componentType, config := range componentConfig {
		// 获取组件的状态规范
		statusSpec := isvc.Status.Components[componentType]

		// 根据部署模式确定Pod标签
		var podLabelKey, podLabelValue string
		// if config.DeploymentMode == constants.RawDeployment {
		// 	// RawDeployment模式：使用app标签
		// 	podLabelKey = "app"
		// 	podLabelValue = config.ObjectMeta.Name
		// } else if config.DeploymentMode == constants.MultiNode {
		// 	// MultiNode模式：使用LeaderWorkerSet标签
		// 	podLabelKey = "leaderworkerset.sigs.k8s.io/name"
		// 	podLabelValue = config.ObjectMeta.Name
		// } else {
		// 	r.log.V(1).Info("Unsupported deployment mode for pod query",
		// 		"componentType", componentType,
		// 		"deploymentMode", config.DeploymentMode)
		// 	continue
		// }
		podLabelKey = "app"
		podLabelValue = config.ObjectMeta.Name

		// 查询Pod列表
		pods, err := isvcutils.ListPodsByLabel(r.client, isvc.Namespace, podLabelKey, podLabelValue)
		if err != nil {
			r.log.Error(err, "Failed to list pods for component",
				"componentType", componentType,
				"labelKey", podLabelKey,
				"labelValue", podLabelValue)
			continue
		}

		// 传播模型状态
		statusReconciler.PropagateModelStatus(&isvc.Status, statusSpec, pods, true)

		r.log.V(1).Info("Propagated model status for component",
			"componentType", componentType,
			"podCount", len(pods.Items))
	}

	// 4. 聚合整体就绪条件
	if len(componentList) > 0 {
		statusReconciler.PropagateCrossComponentStatus(&isvc.Status, componentList, "Ready")
		r.log.Info("Propagated cross-component Ready status",
			"componentCount", len(componentList))
	}

	isvc.Status.ObservedGeneration = rbg.Generation

	r.log.Info("Successfully propagated RBG status to InferenceService",
		"namespace", isvc.Namespace,
		"inferenceService", isvc.Name,
		"componentCount", len(componentList))

	return nil
}

// reconcileRBAC 为Router组件创建RBAC资源
func (r *RBGStrategy) reconcileRBAC(ctx context.Context, isvc *v1beta1.InferenceService, componentConfigs map[v1beta1.ComponentType]*rbgpkg.ComponentConfig) error {
	// 检查是否存在Router组件
	routerConfig, hasRouter := componentConfigs[v1beta1.RouterComponent]
	if !hasRouter {
		r.log.V(1).Info("No router component, skipping RBAC creation")
		return nil
	}

	r.log.Info("Creating RBAC resources for router component",
		"namespace", isvc.Namespace,
		"inferenceService", isvc.Name)

	// 创建RBAC Reconciler
	rbacReconciler := rbac.NewRBACReconciler(
		r.client,
		r.scheme,
		routerConfig.ObjectMeta,
		v1beta1.RouterComponent,
		isvc,
	)

	// 调用Reconcile创建ServiceAccount、Role、RoleBinding
	if err := rbacReconciler.Reconcile(); err != nil {
		return fmt.Errorf("failed to create RBAC resources for router: %w", err)
	}

	// 更新Router的PodSpec，设置ServiceAccountName
	serviceAccountName := rbacReconciler.GetServiceAccountName()
	routerConfig.PodSpec.ServiceAccountName = serviceAccountName

	r.log.Info("Successfully created RBAC resources for router",
		"serviceAccountName", serviceAccountName)

	return nil
}

// reconcileHPA 为每个Role创建独立的HPA资源
func (r *RBGStrategy) reconcileHPA(ctx context.Context, isvc *v1beta1.InferenceService, componentConfigs map[v1beta1.ComponentType]*rbgpkg.ComponentConfig) error {
	r.log.Info("Creating HPA resources for all roles",
		"namespace", isvc.Namespace,
		"inferenceService", isvc.Name)

	// 构建InferenceServiceSpec用于AutoscalerClass判断
	inferenceServiceSpec := &v1beta1.InferenceServiceSpec{
		Predictor: v1beta1.PredictorSpec{
			ComponentExtensionSpec: v1beta1.ComponentExtensionSpec{},
		},
		KedaConfig: isvc.Spec.KedaConfig,
	}

	// 遍历所有组件，为每个组件创建HPA
	for componentType, config := range componentConfigs {
		r.log.V(1).Info("Creating HPA for component",
			"componentType", componentType,
			"name", config.ObjectMeta.Name)

		// 更新inferenceServiceSpec中的ComponentExtensionSpec
		inferenceServiceSpec.Predictor.ComponentExtensionSpec = *config.ComponentExtensionSpec

		// 创建Autoscaler Reconciler（会根据AutoscalerClass创建HPA或KEDA）
		autoscalerReconciler, err := autoscaler.NewAutoscalerReconciler(
			r.client,
			r.clientset,
			r.scheme,
			config.ObjectMeta,
			inferenceServiceSpec,
		)
		if err != nil {
			r.log.Error(err, "Failed to create autoscaler reconciler",
				"componentType", componentType)
			continue
		}

		// 设置OwnerReference
		if err := autoscalerReconciler.Autoscaler.SetControllerReferences(isvc, r.scheme); err != nil {
			r.log.Error(err, "Failed to set owner reference for autoscaler",
				"componentType", componentType)
			continue
		}

		// 调用Reconcile创建HPA/KEDA资源
		if err := autoscalerReconciler.Reconcile(); err != nil {
			r.log.Error(err, "Failed to reconcile autoscaler",
				"componentType", componentType)
			continue
		}

		r.log.Info("Successfully created autoscaler for component",
			"componentType", componentType,
			"name", config.ObjectMeta.Name)
	}

	return nil
}

// reconcilePDB 为每个Role创建独立的PDB资源
func (r *RBGStrategy) reconcilePDB(ctx context.Context, isvc *v1beta1.InferenceService, componentConfigs map[v1beta1.ComponentType]*rbgpkg.ComponentConfig) error {
	r.log.Info("Creating PDB resources for all roles",
		"namespace", isvc.Namespace,
		"inferenceService", isvc.Name)

	// 遍历所有组件，为每个组件创建PDB
	for componentType, config := range componentConfigs {
		r.log.V(1).Info("Creating PDB for component",
			"componentType", componentType,
			"name", config.ObjectMeta.Name)

		// 创建PDB Reconciler
		pdbReconciler := pdb.NewPDBReconciler(
			r.client,
			r.scheme,
			config.ObjectMeta,
			config.ComponentExtensionSpec,
		)

		// 设置OwnerReference
		if err := controllerutil.SetControllerReference(isvc, pdbReconciler.PDB, r.scheme); err != nil {
			r.log.Error(err, "Failed to set owner reference for PDB",
				"componentType", componentType)
			continue
		}

		// 调用Reconcile创建PDB资源
		if _, err := pdbReconciler.Reconcile(); err != nil {
			r.log.Error(err, "Failed to reconcile PDB",
				"componentType", componentType)
			continue
		}

		r.log.Info("Successfully created PDB for component",
			"componentType", componentType,
			"name", config.ObjectMeta.Name)
	}

	return nil
}

func createRawURL(clientset kubernetes.Interface, metadata metav1.ObjectMeta) (*knapis.URL, error) {
	ingressConfig, err := controllerconfig.NewIngressConfig(clientset)
	if err != nil {
		return nil, err
	}

	domainService := services.NewDomainService()
	url := &knapis.URL{}
	url.Scheme = "http"
	url.Host, err = domainService.GenerateDomainName(fmt.Sprintf("s-%s", metadata.Name), metadata, ingressConfig)
	if err != nil {
		return nil, fmt.Errorf("failed creating host name: %w", err)
	}

	return url, nil
}
