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
) (*RBGResult, error) {
	r.log.Info("Reconciling RBG",
		"namespace", isvc.Namespace,
		"name", isvc.Name,
		"componentCount", len(components))

	// 1. 为每个Component生成RoleSpec
	roles, err := r.buildRoles(components)
	if err != nil {
		r.log.Error(err, "Failed to build roles")
		return nil, fmt.Errorf("failed to build roles: %w", err)
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
				return nil, fmt.Errorf("failed to create RBG: %w", err)
			}
			r.log.Info("Created RBG successfully", "name", rbgName)
		} else {
			// 其他错误
			r.log.Error(err, "Failed to get RBG")
			return nil, fmt.Errorf("failed to get RBG: %w", err)
		}
	} else {
		// RBG已存在，更新
		rbg, err = r.updateRBG(ctx, existingRBG, roles)
		if err != nil {
			r.log.Error(err, "Failed to update RBG")
			return nil, fmt.Errorf("failed to update RBG: %w", err)
		}
		r.log.Info("Updated RBG successfully", "name", rbgName)
	}

	// 3. 检查RBG是否Ready
	ready := r.isRBGReady(rbg)

	// 4. 返回结果
	return &RBGResult{
		RBG:   rbg,
		Ready: ready,
		URL:   nil, // URL将在后续集成Service时实现
	}, nil
}

// GetStatus获取RBG当前状态
func (r *RBGReconciler) GetStatus(ctx context.Context, namespace, name string) (*RBGStatus, error) {
	r.log.Info("Getting RBG status", "namespace", namespace, "name", name)

	// 1. 获取RBG资源
	rbg := &workloadsv1alpha1.RoleBasedGroup{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, rbg)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// RBG不存在
			return &RBGStatus{
				RoleStatuses: make(map[v1beta1.ComponentType]*RoleStatus),
				Conditions:   []metav1.Condition{},
			}, nil
		}
		return nil, fmt.Errorf("failed to get RBG: %w", err)
	}

	// 2. 直接访问类型化的状态字段
	roleStatuses := make(map[v1beta1.ComponentType]*RoleStatus)
	for _, roleStatus := range rbg.Status.RoleStatuses {
		componentType := r.roleNameToComponentType(roleStatus.Name)
		roleStatuses[componentType] = &RoleStatus{
			ReadyReplicas:   roleStatus.ReadyReplicas,
			Replicas:        roleStatus.Replicas,
			UpdatedReplicas: roleStatus.UpdatedReplicas,
		}
	}

	// 3. 返回RBGStatus
	return &RBGStatus{
		RoleStatuses: roleStatuses,
		Conditions:   rbg.Status.Conditions,
	}, nil
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
	// 使用类型化对象创建RBG资源
	rbg := &workloadsv1alpha1.RoleBasedGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      isvc.Name,
			Namespace: isvc.Namespace,
			Labels:    r.buildRBGLabels(isvc),
		},
		Spec: workloadsv1alpha1.RoleBasedGroupSpec{
			Roles: roles,
		},
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
func (r *RBGReconciler) updateRBG(ctx context.Context, existing *workloadsv1alpha1.RoleBasedGroup, newRoles []workloadsv1alpha1.RoleSpec) (*workloadsv1alpha1.RoleBasedGroup, error) {
	// 直接更新Spec
	existing.Spec.Roles = newRoles

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

// isRBGReady检查RBG是否Ready
func (r *RBGReconciler) isRBGReady(rbg *workloadsv1alpha1.RoleBasedGroup) bool {
	if rbg == nil {
		return false
	}

	// 检查所有Role是否Ready
	for _, roleStatus := range rbg.Status.RoleStatuses {
		if roleStatus.ReadyReplicas < roleStatus.Replicas {
			r.log.Info("Role not ready",
				"role", roleStatus.Name,
				"readyReplicas", roleStatus.ReadyReplicas,
				"replicas", roleStatus.Replicas)
			return false
		}
	}

	// 检查整体Condition
	for _, condition := range rbg.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}

	// 如果没有Condition，但所有Role都就绪，也认为就绪
	if len(rbg.Status.RoleStatuses) > 0 {
		return true
	}

	return false
}

// roleNameToComponentType将Role名称转换为ComponentType
func (r *RBGReconciler) roleNameToComponentType(roleName string) v1beta1.ComponentType {
	switch roleName {
	case "engine":
		return v1beta1.EngineComponent
	case "decoder":
		return v1beta1.DecoderComponent
	case "router":
		return v1beta1.RouterComponent
	default:
		return v1beta1.ComponentType(roleName)
	}
}
