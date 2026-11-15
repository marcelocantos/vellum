// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The viewer's Markdown editor: CodeMirror 6 with the GFM Lezer parser,
// nested highlighting for fenced code (languages load on first use from
// their own chunks), list continuation, search, folding and multiple
// cursors. chrome.js imports this module the first time Edit is pressed
// and talks to it only through createEditor's handle.

import { Compartment, EditorState, Prec, Text } from "@codemirror/state";
import {
  EditorView,
  crosshairCursor,
  drawSelection,
  dropCursor,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  rectangularSelection,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import {
  HighlightStyle,
  bracketMatching,
  foldGutter,
  foldKeymap,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search";
import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { tags as t } from "@lezer/highlight";

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace';

// Colours follow the viewer's GitHub palette so the editor and the
// rendered page read as one product in either theme.
const palette = {
  light: {
    bg: "#ffffff",
    fg: "#1f2328",
    gutterBg: "#f6f8fa",
    gutterFg: "#8c959f",
    border: "#d1d9e0",
    activeLine: "#f6f8fa",
    selection: "#b6d7ff",
    cursor: "#1f2328",
    match: "#fff8c5",
    heading: "#0550ae",
    emphasis: "#1f2328",
    link: "#0969da",
    url: "#57606a",
    code: "#953800",
    meta: "#6e7781",
    quote: "#57606a",
    keyword: "#cf222e",
    string: "#0a3069",
    number: "#0550ae",
    comment: "#6e7781",
    func: "#8250df",
    type: "#953800",
    invalid: "#cf222e",
  },
  dark: {
    bg: "#0d1117",
    fg: "#e6edf3",
    gutterBg: "#0d1117",
    gutterFg: "#6e7681",
    border: "#30363d",
    activeLine: "#161b22",
    selection: "#264f78",
    cursor: "#e6edf3",
    match: "#3a3420",
    heading: "#79c0ff",
    emphasis: "#e6edf3",
    link: "#58a6ff",
    url: "#8b949e",
    code: "#ffa657",
    meta: "#8b949e",
    quote: "#8b949e",
    keyword: "#ff7b72",
    string: "#a5d6ff",
    number: "#79c0ff",
    comment: "#8b949e",
    func: "#d2a8ff",
    type: "#ffa657",
    invalid: "#f85149",
  },
};

function themeFor(dark) {
  const c = dark ? palette.dark : palette.light;
  const view = EditorView.theme(
    {
      "&": { height: "100%", backgroundColor: c.bg, color: c.fg, fontSize: "13.5px" },
      "&.cm-focused": { outline: "none" },
      ".cm-scroller": { fontFamily: MONO, lineHeight: "1.6" },
      ".cm-content": { caretColor: c.cursor, padding: "12px 0" },
      ".cm-line": { padding: "0 16px 0 8px" },
      ".cm-cursor, .cm-dropCursor": { borderLeftColor: c.cursor },
      "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection":
        { backgroundColor: c.selection },
      ".cm-activeLine": { backgroundColor: c.activeLine },
      ".cm-gutters": { backgroundColor: c.gutterBg, color: c.gutterFg, borderRight: "1px solid " + c.border },
      ".cm-activeLineGutter": { backgroundColor: c.activeLine, color: c.fg },
      ".cm-selectionMatch": { backgroundColor: c.match },
      ".cm-searchMatch": { backgroundColor: c.match, outline: "1px solid " + c.border },
      ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: c.selection },
      ".cm-matchingBracket": { outline: "1px solid " + c.gutterFg },
      ".cm-panels": { backgroundColor: c.gutterBg, color: c.fg, borderColor: c.border },
      ".cm-panels input, .cm-panels button": { font: "inherit", fontSize: "12px" },
      ".cm-textfield": { backgroundColor: c.bg, color: c.fg, border: "1px solid " + c.border, borderRadius: "4px" },
      ".cm-button": { backgroundImage: "none", backgroundColor: c.bg, color: c.fg, border: "1px solid " + c.border, borderRadius: "4px" },
      ".cm-foldPlaceholder": { backgroundColor: c.activeLine, border: "1px solid " + c.border, color: c.gutterFg },
    },
    { dark: dark }
  );
  const style = HighlightStyle.define([
    { tag: t.heading1, color: c.heading, fontWeight: "700", fontSize: "1.25em" },
    { tag: t.heading2, color: c.heading, fontWeight: "700", fontSize: "1.15em" },
    { tag: t.heading3, color: c.heading, fontWeight: "700", fontSize: "1.05em" },
    { tag: [t.heading4, t.heading5, t.heading6], color: c.heading, fontWeight: "700" },
    { tag: t.strong, color: c.emphasis, fontWeight: "700" },
    { tag: t.emphasis, color: c.emphasis, fontStyle: "italic" },
    { tag: t.strikethrough, textDecoration: "line-through" },
    { tag: t.link, color: c.link, textDecoration: "underline" },
    { tag: t.url, color: c.url },
    { tag: t.monospace, color: c.code },
    { tag: t.quote, color: c.quote, fontStyle: "italic" },
    { tag: [t.processingInstruction, t.contentSeparator, t.labelName], color: c.meta },
    { tag: [t.meta, t.documentMeta], color: c.meta },
    { tag: [t.keyword, t.operatorKeyword, t.modifier, t.controlKeyword], color: c.keyword },
    { tag: [t.string, t.special(t.string), t.regexp], color: c.string },
    { tag: [t.number, t.bool, t.null, t.atom], color: c.number },
    { tag: [t.comment, t.lineComment, t.blockComment], color: c.comment, fontStyle: "italic" },
    { tag: [t.function(t.variableName), t.function(t.propertyName)], color: c.func },
    { tag: [t.typeName, t.className, t.namespace, t.tagName], color: c.type },
    { tag: [t.attributeName, t.propertyName], color: c.number },
    { tag: t.invalid, color: c.invalid },
  ]);
  return [view, syntaxHighlighting(style)];
}

// createEditor mounts an editor into opts.parent. Callbacks:
//   onSave()          Mod-s
//   onClose()         Escape when nothing inside the editor wants it
//   onDirty(dirty)    the buffer started or stopped differing from the
//                     last text passed to markSaved / setText
export function createEditor(opts) {
  const theme = new Compartment();
  let eol = detectEOL(opts.doc || "");
  let saved = Text.of(splitLines(opts.doc || ""));
  let dirty = false;

  function updateDirty(state) {
    const now = !state.doc.eq(saved);
    if (now === dirty) return;
    dirty = now;
    if (opts.onDirty) opts.onDirty(dirty);
  }

  const appKeys = keymap.of([
    {
      key: "Mod-s",
      preventDefault: true,
      run: function () {
        if (opts.onSave) opts.onSave();
        return true;
      },
    },
  ]);
  // Lowest precedence: the search panel, a multi-cursor selection and
  // the like consume Escape first; only a bare Escape closes the editor.
  const escapeKey = Prec.lowest(
    keymap.of([
      {
        key: "Escape",
        run: function () {
          if (opts.onClose) opts.onClose();
          return true;
        },
      },
    ])
  );

  const view = new EditorView({
    parent: opts.parent,
    state: EditorState.create({
      doc: saved,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightSpecialChars(),
        history(),
        foldGutter(),
        drawSelection(),
        dropCursor(),
        EditorState.allowMultipleSelections.of(true),
        indentOnInput(),
        bracketMatching(),
        closeBrackets(),
        rectangularSelection(),
        crosshairCursor(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        EditorView.lineWrapping,
        markdown({ base: markdownLanguage, codeLanguages: languages, addKeymap: true }),
        appKeys,
        keymap.of([
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...searchKeymap,
          ...historyKeymap,
          ...foldKeymap,
          indentWithTab,
        ]),
        escapeKey,
        theme.of(themeFor(Boolean(opts.dark))),
        EditorView.contentAttributes.of({ "aria-label": "Markdown source", spellcheck: "true" }),
        EditorView.updateListener.of(function (u) {
          if (u.docChanged) updateDirty(u.state);
        }),
      ],
    }),
  });

  return {
    view: view,
    // The buffer with the file's own line endings restored.
    getText: function () {
      return view.state.doc.sliceString(0, view.state.doc.length, eol);
    },
    // Replace the buffer with text from disk. Only the span that differs
    // is replaced, so the cursor and scroll position survive an outside
    // edit elsewhere in the file, and the change is one undo step.
    setText: function (text) {
      eol = detectEOL(text);
      const next = splitLines(text).join("\n");
      const prev = view.state.doc.toString();
      let start = 0;
      const max = Math.min(prev.length, next.length);
      while (start < max && prev.charCodeAt(start) === next.charCodeAt(start)) start++;
      let end = 0;
      while (
        end < max - start &&
        prev.charCodeAt(prev.length - 1 - end) === next.charCodeAt(next.length - 1 - end)
      ) {
        end++;
      }
      if (start < prev.length - end || start < next.length - end) {
        view.dispatch({
          changes: { from: start, to: prev.length - end, insert: next.slice(start, next.length - end) },
        });
      }
      saved = view.state.doc;
      updateDirty(view.state);
    },
    // The buffer as an immutable value, for markSaved.
    snapshot: function () {
      return view.state.doc;
    },
    // Record snap (from snapshot, taken when a save began) as what is on
    // disk; edits typed while the save was in flight stay dirty.
    markSaved: function (snap) {
      saved = snap || view.state.doc;
      updateDirty(view.state);
    },
    isDirty: function () {
      return dirty;
    },
    setDark: function (dark) {
      view.dispatch({ effects: theme.reconfigure(themeFor(Boolean(dark))) });
    },
    // Put the cursor at the start of 1-based line n, scrolled near the top.
    gotoLine: function (n) {
      const line = view.state.doc.line(Math.max(1, Math.min(view.state.doc.lines, n)));
      view.dispatch({
        selection: { anchor: line.from },
        effects: EditorView.scrollIntoView(line.from, { y: "start", yMargin: 48 }),
      });
    },
    // 1-based line of the main cursor.
    cursorLine: function () {
      return view.state.doc.lineAt(view.state.selection.main.head).number;
    },
    focus: function () {
      view.focus();
    },
    destroy: function () {
      view.destroy();
    },
  };
}

function detectEOL(text) {
  return text.indexOf("\r\n") >= 0 ? "\r\n" : "\n";
}

function splitLines(text) {
  return text.split(/\r\n|\r|\n/);
}
