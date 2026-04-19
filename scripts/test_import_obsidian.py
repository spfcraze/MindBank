#!/usr/bin/env python3
"""Tests for import-obsidian.py parsing logic.

Run: python3 scripts/test_import_obsidian.py
"""

import os
import sys
import tempfile
from pathlib import Path

# Add parent to path
sys.path.insert(0, str(Path(__file__).parent))

import importlib.util

# Load module with hyphenated filename
_spec = importlib.util.spec_from_file_location(
    "import_obsidian",
    str(Path(__file__).parent / "import-obsidian.py"),
)
_mod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_mod)

parse_frontmatter = _mod.parse_frontmatter
extract_wikilinks = _mod.extract_wikilinks
extract_tags = _mod.extract_tags
detect_node_type = _mod.detect_node_type
discover_notes = _mod.discover_notes
parse_note = _mod.parse_note
resolve_namespace = _mod.resolve_namespace
build_topic_nodes = _mod.build_topic_nodes


def test_parse_frontmatter():
    """Parse YAML frontmatter correctly."""
    content = """---
title: My Note
tags:
  - project
  - important
type: decision
date: 2024-01-15
---

This is the body content.
"""
    meta, body = parse_frontmatter(content)
    assert meta["title"] == "My Note", f"Got: {meta.get('title')}"
    assert "project" in meta["tags"], f"Got: {meta.get('tags')}"
    assert "important" in meta["tags"], f"Got: {meta.get('tags')}"
    assert meta["type"] == "decision", f"Got: {meta.get('type')}"
    assert "body content" in body, f"Got: {body[:50]}"
    print("  PASS: test_parse_frontmatter")


def test_parse_frontmatter_empty():
    """Handle notes without frontmatter."""
    content = "Just a regular note with no frontmatter."
    meta, body = parse_frontmatter(content)
    assert meta == {}
    assert body == content
    print("  PASS: test_parse_frontmatter_empty")


def test_parse_frontmatter_scalars():
    """Parse scalar types correctly."""
    content = """---
count: 42
rating: 3.14
active: true
---

Body
"""
    meta, body = parse_frontmatter(content)
    assert meta["count"] == 42
    assert meta["rating"] == 3.14
    assert meta["active"] is True
    print("  PASS: test_parse_frontmatter_scalars")


def test_extract_wikilinks():
    """Extract wikilinks from content."""
    content = """Some text with [[Note A]] and [[Note B|Display]] links.
Also [[Note C#heading]] and [[Note D#heading|Alias]].
No links here except [[Note E]].
"""
    links = extract_wikilinks(content)
    assert "Note A" in links, f"Got: {links}"
    assert "Note B" in links, f"Got: {links}"
    assert "Note C" in links, f"Got: {links}"
    assert "Note D" in links, f"Got: {links}"
    assert "Note E" in links, f"Got: {links}"
    assert len(links) == 5, f"Got {len(links)} links: {links}"
    print("  PASS: test_extract_wikilinks")


def test_extract_wikilinks_edge_cases():
    """Handle edge cases in wikilink parsing."""
    # Empty links
    assert extract_wikilinks("[[]]") == []
    # Triple brackets — regex matches from first [[, captures [Note as content
    links = extract_wikilinks("[[[Note]]]")
    assert len(links) == 1  # one match, content is "[Note"
    # Multiple on same line
    links = extract_wikilinks("[[A]] and [[B]]")
    assert len(links) == 2
    print("  PASS: test_extract_wikilinks_edge_cases")


def test_extract_tags():
    """Extract inline tags."""
    text = "Some text #project and #important notes. Not#atag #valid-tag"
    tags = extract_tags(text)
    assert "project" in tags, f"Got: {tags}"
    assert "important" in tags, f"Got: {tags}"
    assert "valid-tag" in tags, f"Got: {tags}"
    assert "atag" not in tags, f"Got: {tags}"
    print("  PASS: test_extract_tags")


def test_detect_node_type():
    """Detect node type from various sources."""
    # From frontmatter
    assert detect_node_type({"type": "decision"}, set(), "") == "decision"
    # From tags
    assert detect_node_type({}, {"project", "code"}, "") == "project"
    # From folder
    assert detect_node_type({}, set(), "decision") == "decision"
    # Default
    assert detect_node_type({}, set(), "random") == "fact"
    print("  PASS: test_detect_node_type")


def test_discover_notes(tmpdir):
    """Discover .md files, skip hidden dirs."""
    # Create test vault
    vault = Path(tmpdir)
    (vault / "note1.md").write_text("content")
    (vault / "note2.md").write_text("content")
    (vault / ".obsidian").mkdir(exist_ok=True)
    (vault / ".obsidian" / "config.md").write_text("should skip")
    (vault / "sub").mkdir(exist_ok=True)
    (vault / "sub" / "note3.md").write_text("content")

    notes = discover_notes(str(vault))
    assert len(notes) == 3, f"Got {len(notes)} notes: {notes}"
    assert all(".obsidian" not in n for n in notes)
    print("  PASS: test_discover_notes")


def test_parse_note(tmpdir):
    """Parse a complete note."""
    vault = Path(tmpdir)
    note_path = vault / "test-note.md"
    note_path.write_text("""---
type: decision
tags:
  - architecture
---

# Test Note

This is a test with [[Other Note]] and [[Link|Alias]].

#inline-tag
""")

    note = parse_note(str(note_path), str(vault))
    assert note is not None
    assert note["label"] == "test-note"
    assert note["node_type"] == "decision"
    assert "Other Note" in note["wikilinks"]
    assert "Link" in note["wikilinks"]
    assert "inline-tag" in note["tags"]
    assert "architecture" in note["tags"]
    print("  PASS: test_parse_note")


def test_parse_note_empty():
    """Return None for unreadable files."""
    result = parse_note("/nonexistent/file.md", "/nonexistent")
    assert result is None
    print("  PASS: test_parse_note_empty")


def test_resolve_namespace():
    """Resolve namespace from folder or arg."""
    assert resolve_namespace("myproject", "some/folder") == "myproject"
    assert resolve_namespace("", "projects/code") == "projects"
    assert resolve_namespace("", "") == "obsidian"
    print("  PASS: test_resolve_namespace")


def test_build_topic_nodes(tmpdir):
    """Generate topic nodes from folder structure."""
    notes = [
        {"label": "note1", "folder": "projects/code"},
        {"label": "note2", "folder": "projects/code"},
        {"label": "note3", "folder": "projects/design"},
        {"label": "note4", "folder": "personal"},
    ]
    topics = build_topic_nodes(notes, "obsidian")
    labels = [t["label"] for t in topics]
    assert "projects" in labels, f"Got: {labels}"
    assert "code" in labels, f"Got: {labels}"
    assert "design" in labels, f"Got: {labels}"
    assert "personal" in labels, f"Got: {labels}"
    assert all(t["node_type"] == "topic" for t in topics)
    print("  PASS: test_build_topic_nodes")


def run_all():
    """Run all tests."""
    print("Running import-obsidian tests...\n")

    tests = [
        test_parse_frontmatter,
        test_parse_frontmatter_empty,
        test_parse_frontmatter_scalars,
        test_extract_wikilinks,
        test_extract_wikilinks_edge_cases,
        test_extract_tags,
        test_detect_node_type,
        test_parse_note_empty,
        test_resolve_namespace,
    ]

    # Tests needing tmpdir
    with tempfile.TemporaryDirectory() as tmpdir:
        test_discover_notes(tmpdir)
        test_parse_note(tmpdir)

    # Build topic nodes test
    test_build_topic_nodes(None)

    for test in tests:
        test()

    print(f"\nAll {len(tests) + 3} tests passed.")


if __name__ == "__main__":
    run_all()
