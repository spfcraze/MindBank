// === NEURAL GLYPH SYSTEM ===
// Semantic canvas-based glyphs for MindBank graph visualization
// Each glyph is a custom path that evokes the node type's meaning

const GLYPHS = {
  // PROJECT: Rocket launching — projects have momentum, direction, output
  project: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Rocket body (triangle)
    c.beginPath();
    c.moveTo(0, -8);
    c.lineTo(4, 4);
    c.lineTo(0, 2);
    c.lineTo(-4, 4);
    c.closePath();
    c.fillStyle = color;
    c.fill();
    // Window
    c.beginPath();
    c.arc(0, -2, 2, 0, Math.PI * 2);
    c.fillStyle = 'rgba(255,255,255,0.3)';
    c.fill();
    // Flame
    c.beginPath();
    c.moveTo(-2, 4);
    c.lineTo(0, 9 + Math.sin(performance.now() * 0.01) * 2);
    c.lineTo(2, 4);
    c.closePath();
    c.fillStyle = 'rgba(255,160,0,0.8)';
    c.fill();
    c.restore();
  },

  // DECISION: Balance scale — decisions weigh options
  decision: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Central pillar
    c.beginPath();
    c.moveTo(0, -2);
    c.lineTo(0, 6);
    c.lineWidth = 1.5;
    c.strokeStyle = color;
    c.stroke();
    // Crossbar
    c.beginPath();
    c.moveTo(-7, -2);
    c.lineTo(7, -2);
    c.lineWidth = 1;
    c.stroke();
    // Left pan
    c.beginPath();
    c.moveTo(-7, -2);
    c.lineTo(-8, 3);
    c.lineTo(-5, 3);
    c.closePath();
    c.fillStyle = color;
    c.fill();
    // Right pan
    c.beginPath();
    c.moveTo(7, -2);
    c.lineTo(5, 3);
    c.lineTo(8, 3);
    c.closePath();
    c.fillStyle = color;
    c.fill();
    // Base
    c.beginPath();
    c.moveTo(-3, 6);
    c.lineTo(3, 6);
    c.lineWidth = 2;
    c.stroke();
    c.restore();
  },

  // FACT: Solid block with bevel — facts are foundational
  fact: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Main block
    c.beginPath();
    c.rect(-6, -6, 12, 12);
    c.fillStyle = color;
    c.fill();
    // Bevel highlight (top-left)
    c.beginPath();
    c.moveTo(-6, -6);
    c.lineTo(6, -6);
    c.lineTo(5, -5);
    c.lineTo(-5, -5);
    c.lineTo(-5, 5);
    c.lineTo(-6, 6);
    c.closePath();
    c.fillStyle = 'rgba(255,255,255,0.15)';
    c.fill();
    // Bevel shadow (bottom-right)
    c.beginPath();
    c.moveTo(6, -6);
    c.lineTo(6, 6);
    c.lineTo(-6, 6);
    c.lineTo(-5, 5);
    c.lineTo(5, 5);
    c.lineTo(5, -5);
    c.closePath();
    c.fillStyle = 'rgba(0,0,0,0.2)';
    c.fill();
    c.restore();
  },

  // PREFERENCE: Star — favorites, bookmarks
  preference: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    c.beginPath();
    for (let i = 0; i < 5; i++) {
      const a = (i / 5) * Math.PI * 2 - Math.PI / 2;
      const a2 = ((i + 0.5) / 5) * Math.PI * 2 - Math.PI / 2;
      i === 0 ? c.moveTo(Math.cos(a) * 8, Math.sin(a) * 8) : c.lineTo(Math.cos(a) * 8, Math.sin(a) * 8);
      c.lineTo(Math.cos(a2) * 3.5, Math.sin(a2) * 3.5);
    }
    c.closePath();
    c.fillStyle = color;
    c.fill();
    // Inner shine
    c.beginPath();
    c.arc(-1, -1, 2, 0, Math.PI * 2);
    c.fillStyle = 'rgba(255,255,255,0.2)';
    c.fill();
    c.restore();
  },

  // PROBLEM: Warning triangle with exclamation
  problem: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Triangle body
    c.beginPath();
    c.moveTo(0, -8);
    c.lineTo(7, 6);
    c.lineTo(-7, 6);
    c.closePath();
    c.fillStyle = color;
    c.fill();
    // Exclamation stem
    c.beginPath();
    c.moveTo(0, -3);
    c.lineTo(0, 1);
    c.lineWidth = 1.5;
    c.strokeStyle = 'rgba(0,0,0,0.4)';
    c.stroke();
    // Exclamation dot
    c.beginPath();
    c.arc(0, 4, 1, 0, Math.PI * 2);
    c.fillStyle = 'rgba(0,0,0,0.4)';
    c.fill();
    c.restore();
  },

  // ADVICE: Lightbulb — advice illuminates
  advice: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Bulb glass
    c.beginPath();
    c.arc(0, -2, 6, 0, Math.PI * 2);
    c.fillStyle = color;
    c.fill();
    // Filament glow
    c.beginPath();
    c.arc(0, -2, 3, 0, Math.PI * 2);
    c.fillStyle = 'rgba(255,255,200,0.4)';
    c.fill();
    // Base threads
    for (let i = 0; i < 3; i++) {
      c.beginPath();
      c.moveTo(-2, 3 + i * 1.5);
      c.lineTo(2, 3 + i * 1.5);
      c.lineWidth = 0.8;
      c.strokeStyle = 'rgba(255,255,255,0.3)';
      c.stroke();
    }
    c.restore();
  },

  // TOPIC: Hash tag — topics are categories
  topic: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    c.lineWidth = 2;
    c.strokeStyle = color;
    c.lineCap = 'round';
    // Vertical lines
    c.beginPath(); c.moveTo(-3, -7); c.lineTo(-3, 7); c.stroke();
    c.beginPath(); c.moveTo(3, -7); c.lineTo(3, 7); c.stroke();
    // Horizontal lines
    c.beginPath(); c.moveTo(-7, -2); c.lineTo(7, -2); c.stroke();
    c.beginPath(); c.moveTo(-7, 3); c.lineTo(7, 3); c.stroke();
    c.restore();
  },

  // PERSON: Silhouette — head and shoulders
  person: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Head
    c.beginPath();
    c.arc(0, -3, 4, 0, Math.PI * 2);
    c.fillStyle = color;
    c.fill();
    // Shoulders
    c.beginPath();
    c.arc(0, 5, 7, Math.PI, 0);
    c.lineTo(7, 8);
    c.lineTo(-7, 8);
    c.closePath();
    c.fillStyle = color;
    c.fill();
    c.restore();
  },

  // EVENT: Clock — temporal, scheduled
  event: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Outer ring
    c.beginPath();
    c.arc(0, 0, 7, 0, Math.PI * 2);
    c.strokeStyle = color;
    c.lineWidth = 1.5;
    c.stroke();
    // Tick marks
    for (let i = 0; i < 12; i++) {
      const a = (i / 12) * Math.PI * 2 - Math.PI / 2;
      const isMajor = i % 3 === 0;
      const r1 = isMajor ? 5.5 : 6;
      const r2 = 7;
      c.beginPath();
      c.moveTo(Math.cos(a) * r1, Math.sin(a) * r1);
      c.lineTo(Math.cos(a) * r2, Math.sin(a) * r2);
      c.lineWidth = isMajor ? 1.2 : 0.5;
      c.strokeStyle = color;
      c.stroke();
    }
    // Hands
    c.beginPath();
    c.moveTo(0, 0);
    c.lineTo(0, -4);
    c.lineWidth = 1.2;
    c.strokeStyle = color;
    c.stroke();
    c.beginPath();
    c.moveTo(0, 0);
    c.lineTo(3, 2);
    c.lineWidth = 0.8;
    c.stroke();
    // Center dot
    c.beginPath();
    c.arc(0, 0, 1, 0, Math.PI * 2);
    c.fillStyle = color;
    c.fill();
    c.restore();
  },

  // CONCEPT: Hollow diamond frame — abstract framework
  concept: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Outer diamond
    c.beginPath();
    c.moveTo(0, -8);
    c.lineTo(7, 0);
    c.lineTo(0, 8);
    c.lineTo(-7, 0);
    c.closePath();
    c.strokeStyle = color;
    c.lineWidth = 1.5;
    c.stroke();
    // Inner dot
    c.beginPath();
    c.arc(0, 0, 2, 0, Math.PI * 2);
    c.fillStyle = color;
    c.fill();
    c.restore();
  },

  // AGENT: Bot face — artificial, mechanical
  agent: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Head (rounded square)
    c.beginPath();
    c.roundRect(-6, -6, 12, 12, 2);
    c.fillStyle = color;
    c.fill();
    // Eyes
    c.beginPath();
    c.rect(-4, -2, 2.5, 2.5);
    c.rect(1.5, -2, 2.5, 2.5);
    c.fillStyle = 'rgba(255,255,255,0.6)';
    c.fill();
    // Mouth
    c.beginPath();
    c.moveTo(-3, 3);
    c.lineTo(3, 3);
    c.lineWidth = 1;
    c.strokeStyle = 'rgba(255,255,255,0.4)';
    c.stroke();
    // Antenna
    c.beginPath();
    c.moveTo(0, -6);
    c.lineTo(0, -9);
    c.lineWidth = 1;
    c.strokeStyle = color;
    c.stroke();
    c.beginPath();
    c.arc(0, -10, 1.5, 0, Math.PI * 2);
    c.fillStyle = 'rgba(255,255,255,0.3)';
    c.fill();
    c.restore();
  },

  // QUESTION: Query mark — inquiry, search
  question: (c, x, y, s, color) => {
    c.save(); c.translate(x, y); c.scale(s / 10, s / 10);
    // Hook curve
    c.beginPath();
    c.arc(-1, -3, 4, -Math.PI * 0.7, Math.PI * 0.3);
    c.lineWidth = 2.5;
    c.strokeStyle = color;
    c.lineCap = 'round';
    c.stroke();
    // Stem
    c.beginPath();
    c.moveTo(2, -1);
    c.lineTo(-1, 3);
    c.lineWidth = 2.5;
    c.stroke();
    // Dot
    c.beginPath();
    c.arc(-1, 6, 1.5, 0, Math.PI * 2);
    c.fillStyle = color;
    c.fill();
    c.restore();
  }
};

// Helper: draw any glyph by type
function drawGlyph(c, x, y, s, node) {
  const glyphFn = GLYPHS[node.type] || GLYPHS.concept;
  glyphFn(c, x, y, s, node.color);
}

// Export for use in graph.html
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { GLYPHS, drawGlyph };
}
