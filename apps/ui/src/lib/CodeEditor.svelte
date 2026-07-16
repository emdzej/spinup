<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as monaco from 'monaco-editor';
  import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
  import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker';
  import cssWorker from 'monaco-editor/esm/vs/language/css/css.worker?worker';
  import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker';
  import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker';

  // Wire Monaco's web workers into Vite's ?worker mechanism. This runs once
  // per page load — Monaco caches its environment.
  self.MonacoEnvironment = {
    getWorker(_workerId, label) {
      switch (label) {
        case 'json':
          return new jsonWorker();
        case 'css':
        case 'scss':
        case 'less':
          return new cssWorker();
        case 'html':
        case 'handlebars':
        case 'razor':
          return new htmlWorker();
        case 'typescript':
        case 'javascript':
          return new tsWorker();
        default:
          return new editorWorker();
      }
    }
  };

  interface Props {
    filename: string;
    value: string;
    onchange: (v: string) => void;
    height?: number;
  }

  let { filename, value, onchange, height = 380 }: Props = $props();

  let container: HTMLDivElement | undefined = $state();
  let editor: monaco.editor.IStandaloneCodeEditor | undefined;
  let currentModelUri: string | undefined;

  // Extension → Monaco language id. Falls back to plaintext when unknown.
  function languageOf(name: string): string {
    const ext = name.slice(name.lastIndexOf('.') + 1).toLowerCase();
    switch (ext) {
      case 'go': return 'go';
      case 'js': case 'mjs': case 'cjs': return 'javascript';
      case 'ts': return 'typescript';
      case 'rs': return 'rust';
      case 'json': return 'json';
      case 'toml': return 'plaintext'; // Monaco has no built-in TOML
      case 'yaml': case 'yml': return 'yaml';
      case 'md': case 'markdown': return 'markdown';
      case 'html': case 'htm': return 'html';
      case 'css': return 'css';
      case 'sh': case 'bash': return 'shell';
      case 'py': return 'python';
      default: return 'plaintext';
    }
  }

  onMount(() => {
    if (!container) return;
    editor = monaco.editor.create(container, {
      value,
      language: languageOf(filename),
      automaticLayout: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      tabSize: 4,
      padding: { top: 8, bottom: 8 },
      renderLineHighlight: 'gutter',
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true
    });
    currentModelUri = editor.getModel()?.uri.toString();

    editor.onDidChangeModelContent(() => {
      const v = editor?.getValue() ?? '';
      onchange(v);
    });
  });

  onDestroy(() => {
    editor?.getModel()?.dispose();
    editor?.dispose();
  });

  // When the file changes (tab switch), swap the model so undo history / language
  // isolate per file. Preserve existing value if user hadn't saved.
  $effect(() => {
    if (!editor) return;
    // Trigger dependency reads
    const _fn = filename;
    const _val = value;
    const model = editor.getModel();
    if (!model) return;
    // If external value differs from editor's content, update editor (avoid
    // clobbering during ongoing edits by comparing).
    if (model.getValue() !== _val) {
      model.setValue(_val);
    }
    const newLang = languageOf(_fn);
    if (model.getLanguageId() !== newLang) {
      monaco.editor.setModelLanguage(model, newLang);
    }
  });
</script>

<div
  class="w-full border border-t-0 border-border-strong rounded-b-md overflow-hidden"
  bind:this={container}
  style="height: {height}px"
></div>
