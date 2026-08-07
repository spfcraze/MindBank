#!/bin/bash
set -e
cd "$(dirname "$0")/../brain3d-physics"
wasm-pack build --target web --out-dir ../internal/handler/static/pkg/
echo "Brain3D WASM built successfully"