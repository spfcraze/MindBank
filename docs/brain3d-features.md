# Brain3D Visualization

## Overview

The Brain3D view (`internal/handler/static/brain3d.html`) renders the MindBank knowledge graph as an interactive 3D force-directed simulation using Three.js.

## FlowParticle3D

- **3 generations** of particles (gen 1: direct, gen 2: social, gen 3: layered)
- Each generation has distinct color, trail length, and spawn radius
- Trails use `BufferGeometry` with vertex colors
- **Memory:** Call `dispose()` before `reset()` or scene teardown to free GPU resources

## Memory Management

```javascript
// Per-particle cleanup
particle.dispose(); // Disposes geometry, material, removes from scene

// Batch cleanup (e.g., when recreating particles)
this.flowParticles.forEach(p => p.dispose());
```

## Performance Notes

- Max particles: 300 (reduced from 800 for cleaner look)
- Mobile fallback: reduce to 50 particles if `navigator.hardwareConcurrency < 4`
- Use `frustumCulled = false` on trails for off-screen rendering
- Always dispose old particles before creating new ones to prevent GPU memory leaks

## Colors

| Element | Color | Hex |
|---|---|---|
| decision | warm orange | `#ff6b35` |
| fact | teal | `#4ecdc4` |
| problem | purple | `#a78bfa` |
| depends_on | teal | `#4ecdc4` |
| contradicts | orange | `#ff6b35` |
| influences | purple | `#a78bfa` |

## Controls

- **Left click + drag:** Rotate camera
- **Right click + drag:** Pan
- **Scroll:** Zoom
- **Hover node:** Show tooltip with label and type
