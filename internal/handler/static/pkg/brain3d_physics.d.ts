/* tslint:disable */
/* eslint-disable */

export class PhysicsEngine {
    free(): void;
    [Symbol.dispose](): void;
    get_positions(): Float32Array;
    is_settled(): boolean;
    load_graph(node_count: number, node_types: Uint8Array, node_namespaces: Uint32Array, node_importance: Float32Array, edge_sources: Uint32Array, edge_targets: Uint32Array, edge_types: Uint8Array, edge_weights: Float32Array): void;
    constructor();
    node_count(): number;
    set_edge_type_weight(edge_type: number, weight: number): void;
    set_namespace_strength(strength: number): void;
    stabilize(max_steps: number): boolean;
    step(dt: number): void;
}

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_physicsengine_free: (a: number, b: number) => void;
    readonly physicsengine_get_positions: (a: number) => any;
    readonly physicsengine_is_settled: (a: number) => number;
    readonly physicsengine_load_graph: (a: number, b: number, c: number, d: number, e: number, f: number, g: number, h: number, i: number, j: number, k: number, l: number, m: number, n: number, o: number, p: number) => void;
    readonly physicsengine_new: () => number;
    readonly physicsengine_node_count: (a: number) => number;
    readonly physicsengine_set_edge_type_weight: (a: number, b: number, c: number) => void;
    readonly physicsengine_set_namespace_strength: (a: number, b: number) => void;
    readonly physicsengine_stabilize: (a: number, b: number) => number;
    readonly physicsengine_step: (a: number, b: number) => void;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
