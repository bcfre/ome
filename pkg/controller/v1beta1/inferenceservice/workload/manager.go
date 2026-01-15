package workload

import (
	"fmt"
	"sync"

	"github.com/go-logr/logr"

	"github.com/sgl-project/ome/pkg/apis/ome/v1beta1"
)

// WorkloadStrategyManager 管理和选择工作负载策略
type WorkloadStrategyManager struct {
	strategies map[string]WorkloadStrategy
	mu         sync.RWMutex
	log        logr.Logger
}

// NewWorkloadStrategyManager 创建策略管理器实例
func NewWorkloadStrategyManager(log logr.Logger) *WorkloadStrategyManager {
	return &WorkloadStrategyManager{
		strategies: make(map[string]WorkloadStrategy),
		log:        log,
	}
}

// RegisterStrategy 注册工作负载策略
func (m *WorkloadStrategyManager) RegisterStrategy(strategy WorkloadStrategy) error {
	if strategy == nil {
		return fmt.Errorf("cannot register nil strategy")
	}

	name := strategy.GetStrategyName()
	if name == "" {
		return fmt.Errorf("strategy name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.strategies[name]; exists {
		return fmt.Errorf("strategy %s is already registered", name)
	}

	m.strategies[name] = strategy
	m.log.Info("Registered workload strategy", "strategy", name)
	return nil
}

// SelectStrategy 根据条件选择合适的策略
// 策略选择流程：
// 1. 检查annotations中是否指定了deploymentMode
// 2. 遍历所有已注册的策略，找到第一个IsApplicable返回true的策略
// 3. 如果没有找到适用的策略，返回错误
func (m *WorkloadStrategyManager) SelectStrategy(isvc *v1beta1.InferenceService, annotations map[string]string) (WorkloadStrategy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.strategies) == 0 {
		return nil, fmt.Errorf("no workload strategies registered")
	}

	// 遍历所有策略，找到第一个适用的
	// 注意：策略的注册顺序很重要，应该先注册特殊策略（如RBG），最后注册默认策略（SingleComponent）
	for name, strategy := range m.strategies {
		if strategy.IsApplicable(isvc, annotations) {
			m.log.V(1).Info("Selected workload strategy",
				"strategy", name,
				"namespace", isvc.Namespace,
				"inferenceService", isvc.Name)
			return strategy, nil
		}
	}

	// 如果没有策略适用，返回错误
	return nil, fmt.Errorf("no applicable workload strategy found for InferenceService %s/%s", isvc.Namespace, isvc.Name)
}

// GetStrategy 根据名称获取策略实例
func (m *WorkloadStrategyManager) GetStrategy(name string) (WorkloadStrategy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	strategy, exists := m.strategies[name]
	if !exists {
		return nil, fmt.Errorf("strategy %s not found", name)
	}

	return strategy, nil
}

// ListStrategies 列出所有已注册的策略
func (m *WorkloadStrategyManager) ListStrategies() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.strategies))
	for name := range m.strategies {
		names = append(names, name)
	}

	return names
}

// UnregisterStrategy 取消注册策略（主要用于测试）
func (m *WorkloadStrategyManager) UnregisterStrategy(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.strategies[name]; !exists {
		return fmt.Errorf("strategy %s not found", name)
	}

	delete(m.strategies, name)
	m.log.Info("Unregistered workload strategy", "strategy", name)
	return nil
}

// Clear 清空所有已注册的策略（主要用于测试）
func (m *WorkloadStrategyManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.strategies = make(map[string]WorkloadStrategy)
	m.log.Info("Cleared all workload strategies")
}
