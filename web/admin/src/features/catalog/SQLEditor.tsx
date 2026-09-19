import { useEffect, useRef } from "react";
import { Annotation, Compartment, EditorState } from "@codemirror/state";
import { EditorView, basicSetup } from "codemirror";
import { PostgreSQL, sql } from "@codemirror/lang-sql";
import { codeEditorTheme } from "@/features/styles/editor-theme";

/**
 * Controlled SQL editor with syntax highlighting. The editable region is
 * labelled through `labelledBy` because a contenteditable element cannot be
 * the target of `<label htmlFor>`.
 */
export function SQLEditor({
  value,
  onChange,
  disabled = false,
  labelledBy,
  describedBy,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  labelledBy: string;
  describedBy?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const editable = useRef(new Compartment());
  const change = useRef(onChange);
  useEffect(() => {
    change.current = onChange;
  });
  useEffect(() => {
    if (!host.current) return;
    const editor = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          codeEditorTheme("13rem"),
          sql({ dialect: PostgreSQL, upperCaseKeywords: true }),
          EditorView.lineWrapping,
          editable.current.of(editableState(disabled)),
          EditorView.updateListener.of((update) => {
            const external = update.transactions.some((transaction) =>
              transaction.annotation(externalChange),
            );
            if (update.docChanged && !external)
              change.current(update.state.doc.toString());
          }),
          EditorView.contentAttributes.of({
            tabindex: "0",
            "aria-labelledby": labelledBy,
            ...(describedBy ? { "aria-describedby": describedBy } : {}),
          }),
        ],
      }),
    });
    view.current = editor;
    return () => {
      editor.destroy();
      view.current = null;
    };
    // The editor is created once; later props are synchronized below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [labelledBy, describedBy]);
  useEffect(() => {
    const editor = view.current;
    if (!editor || editor.state.doc.toString() === value) return;
    // External resets (cancel, publish) replace the document.
    editor.dispatch({
      changes: { from: 0, to: editor.state.doc.length, insert: value },
      annotations: externalChange.of(true),
    });
  }, [value]);
  useEffect(() => {
    view.current?.dispatch({
      effects: editable.current.reconfigure(editableState(disabled)),
    });
  }, [disabled]);
  return (
    <div
      ref={host}
      className="overflow-hidden rounded-md border text-xs"
      aria-disabled={disabled || undefined}
    />
  );
}

const externalChange = Annotation.define<boolean>();

const editableState = (disabled: boolean) => [
  EditorView.editable.of(!disabled),
  EditorState.readOnly.of(disabled),
];
