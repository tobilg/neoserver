import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags } from "@lezer/highlight";

/** CSS variables follow the console theme without recreating the editor/draft. */
export const codeEditorTheme = (height: string) => [
  EditorView.editorAttributes.of({ class: "style-editor" }),
  EditorView.theme({
    "&": {
      height,
      color: "var(--foreground)",
      backgroundColor: "var(--background)",
    },
    "&.cm-focused": { outline: "2px solid var(--ring)", outlineOffset: "-2px" },
    ".cm-scroller": { overflow: "auto", fontFamily: "var(--font-mono)" },
    ".cm-content": { caretColor: "var(--foreground)" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--foreground)" },
    ".cm-gutters": {
      color: "var(--foreground)",
      backgroundColor: "var(--muted)",
      borderColor: "var(--border)",
    },
    ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "var(--muted)" },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground, .cm-content ::selection":
      { backgroundColor: "var(--editor-selection) !important" },
    ".cm-searchMatch, .cm-searchMatch.cm-searchMatch-selected, .cm-selectionMatch, .cm-matchingBracket":
      {
        backgroundColor: "var(--editor-selection)",
        color: "var(--foreground)",
      },
    ".cm-panels, .cm-tooltip": {
      color: "var(--foreground)",
      backgroundColor: "var(--popover)",
      borderColor: "var(--border)",
    },
  }),
  syntaxHighlighting(
    HighlightStyle.define([
      {
        tag: [tags.comment, tags.meta, tags.processingInstruction],
        color: "var(--syntax-comment)",
      },
      {
        tag: [tags.tagName, tags.typeName, tags.keyword],
        color: "var(--syntax-name)",
      },
      {
        tag: [tags.string, tags.attributeValue, tags.number, tags.bool],
        color: "var(--syntax-value)",
      },
      { tag: tags.attributeName, color: "var(--syntax-attribute)" },
    ]),
  ),
];

export const editorTheme = codeEditorTheme("560px");
