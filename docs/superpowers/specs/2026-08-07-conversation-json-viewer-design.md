# Conversation History JSON Viewer Design

## Overview
Enhance the conversation history dialog (`ConversationDialog`) in usage logs to render JSON content interactively using `@microlink/react-json-view`. When JSON parsing fails or the payload is plain text, the dialog falls back to displaying the raw text inside a formatted code block.

## Key Requirements
1. **Interactive JSON Viewer**: Use `@microlink/react-json-view` for valid JSON strings.
2. **Default Collapsed State**: All JSON nodes are collapsed by default (`collapsed={true}`).
3. **Theme Adaptability**: Dynamically adjust the `react-json-view` theme based on light/dark theme using `next-themes` (`resolvedTheme`).
4. **Fallback Handling**: If `JSON.parse(content)` fails or yields non-object primitives, display the raw string in a `<pre>` block.
5. **Clean Interface**: Hide data types (`displayDataTypes={false}`) and root name (`name={false}`) for a clean presentation.

## Component Design

### Location
[`web/src/features/usage-logs/components/dialogs/conversation-dialog.tsx`](file:///Users/ryan/Code/Go/new-api/web/src/features/usage-logs/components/dialogs/conversation-dialog.tsx)

### Data Flow & Logic
```ts
const parsedJson = useMemo(() => {
  if (!content) return null
  try {
    const obj = JSON.parse(content)
    if (typeof obj === 'object' && obj !== null) {
      return obj
    }
    return null
  } catch {
    return null
  }
}, [content])
```

### Rendering Logic
- If `parsedJson !== null`: Render `<ReactJson src={parsedJson} collapsed={true} theme={isDark ? 'ocean' : 'rsuite'} displayDataTypes={false} name={false} />`.
- Else: Render `<pre className="...">...{content}</pre>`.

## Dependencies
- `@microlink/react-json-view` (already installed in `web/package.json`)
