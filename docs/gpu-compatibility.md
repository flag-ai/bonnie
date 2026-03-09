# GPU Compatibility

BONNIE auto-detects GPUs by probing vendor-specific CLI tools in order: NVIDIA → AMD → Intel. If no GPU tool is found, BONNIE runs in CPU-only mode.

## Support Matrix

| Vendor | Detection Tool | Container Runtime | Status |
|--------|---------------|-------------------|--------|
| NVIDIA | `nvidia-smi` | NVIDIA Container Toolkit (DeviceRequests) | Supported |
| AMD | `rocm-smi` | ROCm device mounts (`/dev/kfd`, `/dev/dri`) | Supported |
| Intel | `xpu-smi` | Device mount (`/dev/dri`) | Supported |
| None | — | CPU-only | Fallback |

## NVIDIA

### Required Host Software

- NVIDIA GPU driver
- `nvidia-smi` (included with driver)
- NVIDIA Container Toolkit (for container GPU passthrough)

### Detection

BONNIE runs:
```
nvidia-smi --query-gpu=index,name,memory.total,memory.free,utilization.gpu --format=csv,noheader,nounits
```

### Container GPU Injection

Uses Docker DeviceRequests:
```json
{
  "DeviceRequests": [{
    "Count": -1,
    "Capabilities": [["gpu"]]
  }]
}
```

This is equivalent to `docker run --gpus all`.

## AMD

### Required Host Software

- AMDGPU driver
- ROCm runtime + `rocm-smi`
- User must be in `video` and `render` groups

### Detection

BONNIE runs:
```
rocm-smi --showid --showuse --showmeminfo vram --json
```

### Container GPU Injection

Mounts devices and adds group access:
- `/dev/kfd` → `/dev/kfd` (KFD interface)
- `/dev/dri` → `/dev/dri` (DRM render nodes)
- Groups: `video`, `render`
- Security: `seccomp=unconfined`

## Intel

### Required Host Software

- Intel GPU driver (i915 or xe)
- Intel XPU Manager + `xpu-smi`

### Detection

BONNIE runs:
```
xpu-smi discovery --dump 1,3,6,7
```

Columns: DeviceID, DeviceName, MemoryPhysicalSize, MemoryFree

### Container GPU Injection

Mounts the DRI device:
- `/dev/dri` → `/dev/dri`
- Groups: `video`, `render`

## CPU-Only Mode

If no GPU tool is detected, BONNIE reports:
```json
{
  "vendor": "none",
  "gpus": null,
  "timestamp": "..."
}
```

Container creation with `"gpu": true` will proceed without GPU device injection. The container will run on CPU only.

## Metrics

GPU metrics are exposed at `/metrics` in Prometheus text format:

```
bonnie_gpu_memory_total_mib{index="0",name="...",vendor="nvidia"} 24564
bonnie_gpu_memory_free_mib{index="0",name="...",vendor="nvidia"} 22000
bonnie_gpu_utilization_percent{index="0",name="...",vendor="nvidia"} 35
bonnie_gpu_count{vendor="nvidia"} 1
```
