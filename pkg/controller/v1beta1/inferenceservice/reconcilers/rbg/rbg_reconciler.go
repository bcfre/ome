package rbg

import (
	"context"
	"fmt"

	workloadsv1alpha1 "github.com/bcfre/rbg-api/api/workloads/v1alpha1"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
	"github.com/sgl-project/ome/pkg/constants"
)

var log = ctrl.Log.WithName("RBGReconciler")

// RBGReconciler负责管理RoleBasedGroup资源
type RBGReconciler struct {
	client client.Client
	scheme *runtime.Scheme
	log    logr.Logger
}

// NewRBGReconciler创建新的RBG Reconciler实例
func NewRBGReconciler(client client.Client, scheme *runtime.Scheme) *RBGReconciler {
	return &RBGReconciler{
		client: client,
		scheme: scheme,
		log:    log,
	}
}

// Reconcile协调RBG资源
// 输入: InferenceService实例和各Component的配置
// 输出: RBGResult包含创建/更新的RBG资源和状态
func (r *RBGReconciler) Reconcile(
	ctx context.Context,
	isvc *v1beta1.InferenceService,
	components map[v1beta1.ComponentType]*ComponentConfig,
) (ctrl.Result, error) {
	r.log.Info("Reconciling RBG",
		"namespace", isvc.Namespace,
		"name", isvc.Name,
		"componentCount", len(components))

	// 1. 为每个Component生成RoleSpec
	roles, err := r.buildRoles(components)
	if err != nil {
		r.log.Error(err, "Failed to build roles")
		return ctrl.Result{}, fmt.Errorf("failed to build roles: %w", err)
	}

	// 2. 获取或创建RBG资源
	rbgName := isvc.Name
	rbgNamespace := isvc.Namespace

	// 使用类型化对象处理RBG资源
	existingRBG := &workloadsv1alpha1.RoleBasedGroup{}
	err = r.client.Get(ctx, types.NamespacedName{
		Name:      rbgName,
		Namespace: rbgNamespace,
	}, existingRBG)

	var rbg *workloadsv1alpha1.RoleBasedGroup
	if err != nil {
		if apierrors.IsNotFound(err) {
			// RBG不存在，创建新的
			rbg, err = r.createRBG(ctx, isvc, roles)
			if err != nil {
				r.log.Error(err, "Failed to create RBG")
				return ctrl.Result{}, fmt.Errorf("failed to create RBG: %w", err)
			}
			r.log.Info("Created RBG successfully", "name", rbgName)
		} else {
			// 其他错误
			r.log.Error(err, "Failed to get RBG")
			return ctrl.Result{}, fmt.Errorf("failed to get RBG: %w", err)
		}
	} else {
		// RBG已存在，更新
		rbg, err = r.updateRBG(ctx, existingRBG, roles)
		if err != nil {
			r.log.Error(err, "Failed to update RBG")
			return ctrl.Result{}, fmt.Errorf("failed to update RBG: %w", err)
		}
		r.log.Info("Updated RBG successfully", "name", rbgName)
	}
	// todo: update isvc status
	r.log.Info(rbg.Name)

	// 4. 返回结果
	return ctrl.Result{}, nil
}

// buildRoles为所有Component构建RoleSpec
func (r *RBGReconciler) buildRoles(components map[v1beta1.ComponentType]*ComponentConfig) ([]workloadsv1alpha1.RoleSpec, error) {
	roles := make([]workloadsv1alpha1.RoleSpec, 0, len(components))

	for componentType, config := range components {
		r.log.Info("Building role for component",
			"componentType", componentType,
			"deploymentMode", config.DeploymentMode)

		role, err := buildRoleSpec(config)
		if err != nil {
			return nil, fmt.Errorf("failed to build role for %s: %w", componentType, err)
		}
		roles = append(roles, *role)
	}

	return roles, nil
}

// createRBG创建新的RBG资源
func (r *RBGReconciler) createRBG(ctx context.Context, isvc *v1beta1.InferenceService, roles []workloadsv1alpha1.RoleSpec) (*workloadsv1alpha1.RoleBasedGroup, error) {
	// 提取所有角色名称用于协调配置
	roleNames := make([]string, 0, len(roles))
	for _, role := range roles {
		roleNames = append(roleNames, role.Name)
	}

	// 构建协调配置：设置所有角色间的更新 skew 最大为 1%
	coordinationRequirements := r.buildCoordinationRequirements(roleNames)

	// 使用类型化对象创建RBG资源
	rbg := &workloadsv1alpha1.RoleBasedGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      isvc.Name,
			Namespace: isvc.Namespace,
			Labels:    r.buildRBGLabels(isvc),
		},
		Spec: workloadsv1alpha1.RoleBasedGroupSpec{
			Roles:                    roles,
			CoordinationRequirements: coordinationRequirements,
		},
	}

	// 为每个Role设置默认的RolloutStrategy
	for i := range rbg.Spec.Roles {
		if rbg.Spec.Roles[i].RolloutStrategy == nil {
			rbg.Spec.Roles[i].RolloutStrategy = &workloadsv1alpha1.RolloutStrategy{
				Type: workloadsv1alpha1.RollingUpdateStrategyType,
			}
		}
		if rbg.Spec.Roles[i].RolloutStrategy.RollingUpdate == nil {
			rbg.Spec.Roles[i].RolloutStrategy.RollingUpdate = &workloadsv1alpha1.RollingUpdate{}
		}
		rbg.Spec.Roles[i].RolloutStrategy.RollingUpdate.Type = workloadsv1alpha1.InPlaceIfPossibleUpdateStrategyType
	}

	// 设置OwnerReference，实现级联删除
	if err := controllerutil.SetControllerReference(isvc, rbg, r.scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference: %w", err)
	}

	// 创建RBG资源
	if err := r.client.Create(ctx, rbg); err != nil {
		return nil, fmt.Errorf("failed to create RBG resource: %w", err)
	}

	return rbg, nil
}

// updateRBG更新现有RBG资源
// 注意：保留现有的Replicas配置，因为HPA可能正在管理副本数
func (r *RBGReconciler) updateRBG(ctx context.Context, existing *workloadsv1alpha1.RoleBasedGroup, newRoles []workloadsv1alpha1.RoleSpec) (*workloadsv1alpha1.RoleBasedGroup, error) {
	// 保留现有各个Role的Replicas配置
	// 构建一个map以便快速查找现有Role的Replicas
	existingReplicasMap := make(map[string]*int32)
	for _, existingRole := range existing.Spec.Roles {
		if existingRole.Replicas != nil {
			existingReplicasMap[existingRole.Name] = existingRole.Replicas
		}
	}

	// 更新newRoles，保留现有的Replicas值
	for i := range newRoles {
		if existingReplicas, exists := existingReplicasMap[newRoles[i].Name]; exists {
			// 使用现有的Replicas值，而不是新提供的值
			newRoles[i].Replicas = existingReplicas
			r.log.V(1).Info("Preserving existing replicas for role",
				"roleName", newRoles[i].Name,
				"replicas", *existingReplicas)
		}
	}

	// 提取所有角色名称用于协调配置
	roleNames := make([]string, 0, len(newRoles))
	for _, role := range newRoles {
		roleNames = append(roleNames, role.Name)
	}

	// 构建协调配置：设置所有角色间的更新 skew 最大为 1%
	coordinationRequirements := r.buildCoordinationRequirements(roleNames)

	// 更新Spec
	existing.Spec.Roles = newRoles
	existing.Spec.CoordinationRequirements = coordinationRequirements

	// 为每个Role设置默认的RolloutStrategy
	for i := range existing.Spec.Roles {
		if existing.Spec.Roles[i].RolloutStrategy == nil {
			existing.Spec.Roles[i].RolloutStrategy = &workloadsv1alpha1.RolloutStrategy{
				Type: workloadsv1alpha1.RollingUpdateStrategyType,
			}
		}
		if existing.Spec.Roles[i].RolloutStrategy.RollingUpdate == nil {
			existing.Spec.Roles[i].RolloutStrategy.RollingUpdate = &workloadsv1alpha1.RollingUpdate{}
		}
		existing.Spec.Roles[i].RolloutStrategy.RollingUpdate.Type = workloadsv1alpha1.InPlaceIfPossibleUpdateStrategyType
	}

	// 更新资源
	if err := r.client.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("failed to update RBG resource: %w", err)
	}

	return existing, nil
}

// buildRBGLabels构建RBG的标签
func (r *RBGReconciler) buildRBGLabels(isvc *v1beta1.InferenceService) map[string]string {
	labels := make(map[string]string)

	// 复制InferenceService的标签
	if isvc.Labels != nil {
		for k, v := range isvc.Labels {
			labels[k] = v
		}
	}

	// 添加标识标签
	labels["app"] = isvc.Name
	labels["inferenceservice"] = isvc.Name
	labels["deploymentMode"] = string(constants.RoleBasedGroup)

	return labels
}

// buildCoordinationRequirements构建角色间的协调配置
// 设置所有角色间的更新 skew 最大为 1%，确保滚动更新时各角色保持同步
func (r *RBGReconciler) buildCoordinationRequirements(roleNames []string) []workloadsv1alpha1.Coordination {
	// 如果只有一个角色，不需要协调配置
	if len(roleNames) <= 1 {
		return nil
	}

	// 设置 MaxSkew 为 1%
	maxSkew := "1%"

	coordination := workloadsv1alpha1.Coordination{
		Name:  "all-roles-coordination",
		Roles: roleNames,
		Strategy: &workloadsv1alpha1.CoordinationStrategy{
			RollingUpdate: &workloadsv1alpha1.CoordinationRollingUpdate{
				MaxSkew: &maxSkew,
			},
		},
	}

	return []workloadsv1alpha1.Coordination{coordination}
}
