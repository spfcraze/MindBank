// observer-tab.js
class ObserverTab {
  constructor() {
    this.container = document.getElementById('observer-graph');
    this.statsEl = document.getElementById('observer-stats');
    this.blindspotsEl = document.getElementById('observer-blindspots');
    document.getElementById('observer-trace-btn').addEventListener('click', () => this.trace());
    document.getElementById('observer-seed').addEventListener('keydown', (e) => {
      if (e.key === 'Enter') this.trace();
    });
  }

  async trace() {
    const seed = document.getElementById('observer-seed').value.trim();
    if (!seed) return;

    this.statsEl.innerHTML = '<div style="padding:10px 14px;font-size:12px;color:#888">Tracing dependence...</div>';
    this.container.innerHTML = '';
    this.blindspotsEl.innerHTML = '';

    const isUUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(seed);
    const body = isUUID ? { node_id: seed, max_depth: 3 } : { query: seed, max_depth: 3 };

    try {
      const res = await fetch('/api/v1/analyze/dependence', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });
      const data = await res.json();
      if (!res.ok) {
        this.statsEl.innerHTML = `<div style="color:#ff4444;padding:10px 14px;font-size:12px">Error: ${esc(data.error || 'unknown')}</div>`;
        return;
      }

      this.renderStats(data);
      this.renderGraph(data.dependence_graph, data.seed ? data.seed.id : null);
      this.renderBlindSpots(data.blind_spots);
    } catch (e) {
      this.statsEl.innerHTML = `<div style="color:#ff4444;padding:10px 14px;font-size:12px">Error: ${esc(e.message)}</div>`;
    }
  }

  renderStats(data) {
    const nodes = data.dependence_graph && data.dependence_graph.nodes ? data.dependence_graph.nodes.length : 0;
    const edges = data.dependence_graph && data.dependence_graph.edges ? data.dependence_graph.edges.length : 0;
    this.statsEl.innerHTML = `
      <div class="grid" style="grid-template-columns:repeat(4,1fr);margin-bottom:12px;padding:0 14px">
        <div class="card"><h3>Seed</h3><div class="val" style="font-size:14px">${esc(data.seed && data.seed.label ? data.seed.label : '—')}</div><div class="sub">${esc(data.seed && data.seed.node_type ? data.seed.node_type : '')}</div></div>
        <div class="card"><h3>Critical Depth</h3><div class="val">${data.critical_depth !== undefined ? data.critical_depth : '—'}</div></div>
        <div class="card"><h3>Coverage</h3><div class="val">${data.coverage !== undefined ? (data.coverage * 100).toFixed(1) + '%' : '—'}</div></div>
        <div class="card"><h3>Graph</h3><div class="val" style="font-size:14px">${nodes} / ${edges}</div><div class="sub">nodes / edges</div></div>
      </div>
    `;
  }

  renderGraph(graph, seedId) {
    this.container.innerHTML = '';
    if (!graph || !graph.nodes || graph.nodes.length === 0) {
      this.container.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;height:100%;color:#555;font-size:12px">No graph data</div>';
      return;
    }

    const canvas = document.createElement('canvas');
    canvas.width = this.container.clientWidth || 800;
    canvas.height = this.container.clientHeight || 500;
    this.container.appendChild(canvas);
    const ctx = canvas.getContext('2d');

    const nodes = graph.nodes.map(n => ({ ...n, x: Math.random() * canvas.width, y: Math.random() * canvas.height }));
    const nodeMap = {};
    nodes.forEach(n => nodeMap[n.id] = n);

    // Simple force-directed layout
    for (let iter = 0; iter < 100; iter++) {
      // Repulsion
      for (let i = 0; i < nodes.length; i++) {
        for (let j = i + 1; j < nodes.length; j++) {
          const dx = nodes[j].x - nodes[i].x;
          const dy = nodes[j].y - nodes[i].y;
          const dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const force = 500 / (dist * dist);
          const fx = (dx / dist) * force;
          const fy = (dy / dist) * force;
          nodes[i].x -= fx; nodes[i].y -= fy;
          nodes[j].x += fx; nodes[j].y += fy;
        }
      }
      // Attraction along edges
      (graph.edges || []).forEach(e => {
        const s = nodeMap[e.source_id];
        const t = nodeMap[e.target_id];
        if (!s || !t) return;
        const dx = t.x - s.x;
        const dy = t.y - s.y;
        const dist = Math.sqrt(dx * dx + dy * dy) || 1;
        const force = dist * 0.01;
        const fx = (dx / dist) * force;
        const fy = (dy / dist) * force;
        s.x += fx; s.y += fy;
        t.x -= fx; t.y -= fy;
      });
      // Center gravity
      nodes.forEach(n => {
        n.x += (canvas.width / 2 - n.x) * 0.01;
        n.y += (canvas.height / 2 - n.y) * 0.01;
      });
      // Keep within bounds
      nodes.forEach(n => {
        n.x = Math.max(20, Math.min(canvas.width - 20, n.x));
        n.y = Math.max(20, Math.min(canvas.height - 20, n.y));
      });
    }

    // Draw edges
    ctx.strokeStyle = '#888';
    ctx.lineWidth = 1;
    (graph.edges || []).forEach(e => {
      const s = nodeMap[e.source_id];
      const t = nodeMap[e.target_id];
      if (!s || !t) return;
      ctx.beginPath();
      ctx.moveTo(s.x, s.y);
      ctx.lineTo(t.x, t.y);
      ctx.stroke();
    });

    // Draw nodes
    nodes.forEach(n => {
      ctx.beginPath();
      ctx.arc(n.x, n.y, 8, 0, Math.PI * 2);
      ctx.fillStyle = n.id === seedId ? '#ff4444' : '#0088ff';
      ctx.fill();
      ctx.fillStyle = '#e0e0e0';
      ctx.font = '12px sans-serif';
      ctx.fillText(n.label || n.id.substring(0, 8), n.x + 10, n.y + 4);
    });
  }

  renderBlindSpots(spots) {
    if (!spots || spots.length === 0) {
      this.blindspotsEl.innerHTML = '<div style="color:#00ff88;font-size:12px;padding:10px 14px">✓ No blind spots detected</div>';
      return;
    }
    this.blindspotsEl.innerHTML = '<h3 style="font-size:12px;color:#888;padding:10px 14px 6px;text-transform:uppercase;letter-spacing:1px">Blind Spots</h3>' + spots.map(s => {
      const color = s.severity === 'high' ? '#ff4444' : s.severity === 'medium' ? '#ffaa00' : '#00ff88';
      return `<div style="padding:8px 14px;border-bottom:1px solid #16162a;font-size:12px;color:#e0e0e0;border-left:3px solid ${color}">${esc(s.description)}</div>`;
    }).join('');
  }
}

window.observerTab = new ObserverTab();
