# Workload Strategy Layer

工作负载策略层为OME提供了统一的工作负载管理抽象，支持多种All-in-One工作负载类型（如RBG、JobSet等）。

## 架构概览

```
InferenceService Controller
         ↓
WorkloadStrategyManager (策略管理器)
         ↓
    策略选择
    /      \
SingleComponent  RBG Strategy  (Future Strategies...)
   Strategy         ↓
      ↓          RBGReconciler
   Component
  Reconcilers
```

## 核心组件

### 1. WorkloadStrategy接口

定义所有工作负载策略必须实现的方法：

- `GetStrategyName()`: 返回策略名称
- `IsApplicable()`: 判断策略是否适用
- `ValidateDeploymentModes()`: 验证部署模式
- `ReconcileWorkload()`: 执行工作负载调谐
- `GetIngressDeploymentMode()`: 确定Ingress部署模式
- `PropagateStatus()`: 状态传播

### 2. WorkloadStrategyManager

策略管理器负责：
- 策略注册
- 策略选择
- 策略查询

### 3. 策略实现

#### SingleComponentStrategy

- **适用场景**: 默认策略，传统的单组件独立部署
- **支持的部署模式**: 全部（RawDeployment、MultiNode、MultiNodeRayVLLM、Serverless）
- **特点**: 完全复用现有组件Reconcile逻辑

#### RBGStrategy

- **适用场景**: RBG All-in-One工作负载，通过annotation显式指定
- **支持的部署模式**: RawDeployment、MultiNode
- **特点**: 使用ComponentConfigExtractor从组件提取配置，复用RBGReconciler

## 使用方式

### 在控制器中初始化

```go
// 在InferenceServiceReconciler中添加字段
type InferenceServiceReconciler struct {
    // ... existing fields ...
    StrategyManager *workload.WorkloadStrategyManager
}

// 在SetupWithManager中初始化
func (r *InferenceServiceReconciler) SetupWithManager(mgr ctrl.Manager, ...) error {
    // 创建策略管理器
    r.StrategyManager = workload.NewWorkloadStrategyManager(r.Log)
    
    // 注册RBG策略
    rbgStrategy := workload.NewRBGStrategy(r.Client, r.Scheme, r.Log)
    if err := r.StrategyManager.RegisterStrategy(rbgStrategy); err != nil {
        return err
    }
    
    // 注册单组件策略（作为默认策略，最后注册）
    singleStrategy := workload.NewSingleComponentStrategy(r.Log)
    if err := r.StrategyManager.RegisterStrategy(singleStrategy); err != nil {
        return err
    }
    
    // ... rest of setup ...
}
```

### 在Reconcile中使用

```go
func (r *InferenceServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ... existing code: fetch isvc, reconcile model, select runtime, merge specs ...
    
    // Step 5: 选择工作负载策略
    annotations := utils.Filter(isvc.Annotations, func(key string) bool {
        return !utils.Includes(constants.ServiceAnnotationDisallowedList, key)
    })
    
    strategy, err := r.StrategyManager.SelectStrategy(isvc, annotations)
    if err != nil {
        r.Log.Error(err, "Failed to select workload strategy")
        return reconcile.Result{}, err
    }
    
    r.Log.Info("Selected workload strategy", 
        "strategy", strategy.GetStrategyName(),
        "namespace", isvc.Namespace,
        "inferenceService", isvc.Name)
    
    // Step 6: 验证部署模式
    deploymentModes := &workload.ComponentDeploymentModes{
        Engine:  engineDeploymentMode,
        Decoder: decoderDeploymentMode,
        Router:  routerDeploymentMode,
    }
    
    if err := strategy.ValidateDeploymentModes(deploymentModes); err != nil {
        r.Log.Error(err, "Deployment mode validation failed")
        r.Recorder.Eventf(isvc, v1.EventTypeWarning, "InvalidDeploymentMode", err.Error())
        return reconcile.Result{}, err
    }
    
    // Step 7: 构建WorkloadReconcileRequest
    request := &workload.WorkloadReconcileRequest{
        InferenceService:             isvc,
        BaseModel:                    baseModel,
        BaseModelMeta:                baseModelMeta,
        Runtime:                      rt,
        RuntimeName:                  rtName,
        MergedEngine:                 mergedEngine,
        MergedDecoder:                mergedDecoder,
        MergedRouter:                 mergedRouter,
        DeploymentModes:              deploymentModes,
        ComponentBuilderFactory:      componentBuilderFactory,
        UserSpecifiedRuntime:         userSpecifiedRuntime,
        EngineAcceleratorClass:       engineAC,
        EngineAcceleratorClassName:   engineAcName,
        DecoderAcceleratorClass:      decoderAC,
        DecoderAcceleratorClassName:  decoderAcName,
        EngineSupportedModelFormat:   engineSupportedModelFormats,
        DecoderSupportedModelFormat:  decoderSupportedModelFormats,
    }
    
    // Step 8: 执行工作负载调谐
    result, err := strategy.ReconcileWorkload(ctx, request)
    if err != nil {
        r.Log.Error(err, "Failed to reconcile workload")
        return reconcile.Result{}, err
    }
    
    // 如果需要重新入队，直接返回
    if result.Result.Requeue || result.Result.RequeueAfter > 0 {
        return result.Result, nil
    }
    
    // Step 9: 确定Ingress部署模式
    ingressDeploymentMode := result.IngressDeploymentMode
    
    // Step 10: 调谐Ingress和ExternalService
    // ... existing ingress reconciliation code ...
    
    // Step 11: 清理逻辑
    // ... existing cleanup code ...
    
    // Step 12: 更新状态
    if err := r.updateStatus(isvc, deploymentMode); err != nil {
        return reconcile.Result{}, err
    }
    
    return ctrl.Result{}, nil
}
```

## 扩展新策略

### 添加新的All-in-One工作负载策略（如JobSet）

1. **创建策略类**:

```go
// workload/jobset_strategy.go
type JobSetStrategy struct {
    client client.Client
    scheme *runtime.Scheme
    log    logr.Logger
}

func NewJobSetStrategy(client client.Client, scheme *runtime.Scheme, log logr.Logger) *JobSetStrategy {
    return &JobSetStrategy{
        client: client,
        scheme: scheme,
        log:    log,
    }
}
```

2. **实现WorkloadStrategy接口**:

```go
func (j *JobSetStrategy) GetStrategyName() string {
    return "JobSet"
}

func (j *JobSetStrategy) IsApplicable(isvc *v1beta1.InferenceService, annotations map[string]string) bool {
    if mode, found := annotations[constants.DeploymentMode]; found {
        return mode == "JobSet"
    }
    return false
}

// ... 实现其他接口方法 ...
```

3. **在控制器中注册**:

```go
jobsetStrategy := workload.NewJobSetStrategy(r.Client, r.Scheme, r.Log)
if err := r.StrategyManager.RegisterStrategy(jobsetStrategy); err != nil {
    return err
}
```

## 代码复用策略

### ComponentConfigExtractor接口

所有组件（Engine、Decoder、Router）都实现了`ComponentConfigExtractor`接口，提供配置提取方法：

- `GetPodSpec(isvc)`: 获取主PodSpec
- `GetWorkerPodSpec(isvc)`: 获取Worker PodSpec
- `GetObjectMeta(isvc)`: 获取ObjectMeta
- `GetComponentExtension()`: 获取扩展配置
- `GetWorkerSize()`: 获取Worker数量

RBGStrategy等All-in-One策略通过这些方法提取配置，避免重复实现PodSpec构建逻辑。

### 状态传播

- **SingleComponentStrategy**: 状态已在各组件的Reconcile过程中直接更新到isvc.Status.Components
- **RBGStrategy**: 从RBG.Status提取状态并映射到InferenceService.Status.Components

## 测试

### 单元测试

每个策略和管理器都应有相应的单元测试：

- `single_component_strategy_test.go`
- `rbg_strategy_test.go`
- `manager_test.go`

### 集成测试

在`tests/`目录下添加端到端测试，验证：

- 标准单组件部署
- RBG部署
- 策略切换场景

## 向后兼容性

- **无annotation**: 默认使用SingleComponentStrategy
- **annotation: RoleBasedGroup**: 使用RBGStrategy
- 所有现有InferenceService资源无需修改即可工作

## 性能考虑

- 策略选择基于annotation检查，性能影响可忽略
- 配置提取复用现有逻辑，无额外开销
- 策略管理器使用读写锁，支持并发访问
