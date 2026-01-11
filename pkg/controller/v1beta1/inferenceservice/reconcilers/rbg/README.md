# RBG (Role-Based Group) Workload Integration

## Overview

This directory contains the implementation of RBG workload integration for OME (Open Model Engine), enabling unified management of multiple component roles (engine, decoder, router) within a single RoleBasedGroup resource.

## Implementation Status

### Phase 1: MVP (✅ Completed)

The following core tasks have been completed:

1. ✅ **Constants Definition** - Added `RoleBasedGroup` deployment mode constant
2. ✅ **Type Definitions** - Created RBG Reconciler types (ComponentConfig, RBGResult, RBGStatus)
3. ✅ **RBG Reconciler Base** - Implemented RBGReconciler structure and interfaces
4. ✅ **Role Conversion Logic** - Implemented Component to RBG Role conversion for both RawDeployment and MultiNode modes
5. ✅ **RBG Resource Management** - Implemented RBG resource creation and update using unstructured API
6. ✅ **Status Propagation** - Implemented status propagation from RBG to InferenceService
7. ✅ **Integration Interface** - Created ReconcileRoleBasedGroupDeployment method in DeploymentReconciler

### Pending Items

- ⏸️ **RBG Dependency** - Need to confirm correct RBG Go module path and add to go.mod
- 🔄 **Full Controller Integration** - Complete integration in InferenceService controller to collect component configs and invoke RBG reconciler

## Architecture

### Key Components

1. **RBGReconciler** (`rbg_reconciler.go`)
   - Manages RoleBasedGroup custom resources
   - Converts InferenceService components to RBG roles
   - Handles RBG lifecycle (create, update, delete)

2. **Role Builder** (`role_builder.go`)
   - Converts ComponentConfig to RBG RoleSpec
   - Supports both RawDeployment (StatefulSet) and MultiNode (LeaderWorkerSet) modes
   - Handles label, annotation, and replica configuration

3. **Status Reconciler** (`status_reconciler.go`)
   - Propagates RBG status to InferenceService status
   - Maps RBG role status to component status

## Usage

### Enabling RBG Mode

RBG mode is **opt-in** via annotation. Add the following annotation to your InferenceService:

```yaml
metadata:
  annotations:
    ome.io/deploymentMode: "RoleBasedGroup"
```

### Examples

See the `config/samples/rbg/` directory for complete examples:

1. **Single Engine** (`rbg-single-engine.yaml`)
   - Single engine component with RBG deployment
   - Uses StatefulSet as underlying workload

2. **Engine + Router** (`rbg-engine-router.yaml`)
   - Multiple components in single RBG
   - Demonstrates multi-role deployment

3. **PD-Disaggregated** (`rbg-pd-disaggregated.yaml`)
   - MultiNode engine (LeaderWorkerSet) + RawDeployment decoder
   - Shows mixed workload types in RBG

## Component to Role Mapping

| OME Component | RBG Role Name | Workload Type | Notes |
|---------------|---------------|---------------|-------|
| Engine (Raw) | engine | StatefulSet | For non-distributed inference |
| Engine (MultiNode) | engine | LeaderWorkerSet | For distributed inference |
| Decoder (Raw) | decoder | StatefulSet | For decode-only scenarios |
| Decoder (MultiNode) | decoder | LeaderWorkerSet | For distributed decoding |
| Router | router | StatefulSet | Request routing component |

## Technical Details

### RBG Resource Structure

```yaml
apiVersion: workload.sigs.k8s.io/v1alpha1
kind: RoleBasedGroup
metadata:
  name: <inferenceservice-name>
spec:
  roles:
  - name: engine
    replicas: 2
    workload:
      apiVersion: "apps/v1"
      kind: "StatefulSet"
    template:
      spec: <PodSpec>
```

### Status Propagation

RBG status flows through the following path:

```
RBG Controller → RBG Status → RBGReconciler.GetStatus() 
→ StatusReconciler.PropagateRBGStatus() → InferenceService Status
```

### Temporary Type Handling

Due to pending RBG dependency confirmation, the implementation uses:
- `unstructured.Unstructured` for RBG resource operations
- Temporary `RoleSpec` and `WorkloadSpec` type definitions
- These will be replaced with actual RBG types once dependency is added

## Limitations (MVP)

1. **No Ingress Support** - Network exposure not implemented in MVP
2. **No PodGroup** - Gang scheduling not supported
3. **No Autoscaling** - HPA/KEDA not supported with RBG mode
4. **No Serverless** - Scale-to-zero not supported in RBG

## Future Phases

### Phase 2: MultiNode Support
- Full LeaderWorkerSet configuration
- Leader/Worker PodSpec differentiation
- Worker size calculation

### Phase 3: Multi-Component Coordination
- Multiple components in single RBG
- Mixed workload types (StatefulSet + LWS)
- Startup dependency handling

### Phase 4: Production Readiness
- Error handling and retry logic
- Comprehensive logging and events
- Resource cleanup
- Metrics and monitoring
- Test coverage (70%+)

## Contributing

When working on RBG integration:

1. Follow the existing code structure in `pkg/controller/v1beta1/inferenceservice/reconcilers/rbg/`
2. Update this README when adding new features
3. Add examples to `config/samples/rbg/` for new use cases
4. Ensure backward compatibility - RBG mode is opt-in only

## References

- Design Document: `.qoder/quests/rbg-workload-integration.md`
- RBG Project: https://github.com/sgl-project/rbg
- LeaderWorkerSet: https://github.com/kubernetes-sigs/lws
