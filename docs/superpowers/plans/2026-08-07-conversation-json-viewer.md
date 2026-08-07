# Conversation History JSON Viewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate `@microlink/react-json-view` into `ConversationDialog` to display formatted, collapsible JSON payloads with fallback to raw text if JSON parsing fails.

**Architecture:** Parse the string conversation content into a JSON object inside `ConversationDialog`. If valid, render `<ReactJson>` with `collapsed={true}` and dynamic theme support (`next-themes`); otherwise render formatted `<pre>`.

**Tech Stack:** React 19, TypeScript, `@microlink/react-json-view`, `next-themes`, Tailwind CSS.

## Global Constraints

- Must use `@microlink/react-json-view` for JSON visualization.
- JSON nodes must be collapsed by default (`collapsed={true}`).
- Fall back to raw text if JSON parsing fails.
- Support dark/light mode dynamically via `next-themes` (`resolvedTheme`).
- Frontend i18n copy rules must be followed.

---

### Task 1: Update ConversationDialog Component

**Files:**
- Modify: `web/src/features/usage-logs/components/dialogs/conversation-dialog.tsx`

**Interfaces:**
- Consumes: `logId`, `open`, `onOpenChange` props for `ConversationDialog`
- Produces: Enhanced `ConversationDialog` rendering collapsible JSON with `@microlink/react-json-view` or raw text fallback

- [ ] **Step 1: Update ConversationDialog implementation**

```tsx
import DynamicReactJson from '@microlink/react-json-view'
import { useTheme } from 'next-themes'

// Parse JSON safely
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

// Render parsedJson using DynamicReactJson with collapsed={true} and theme={isDark ? 'ocean' : 'rsuite'}
// If parsedJson is null, render raw text in <pre>
```

- [ ] **Step 2: Verify frontend type check & build**

Run: `cd web && bun run build`
Expected: PASS with 0 errors.

- [ ] **Step 3: Commit changes**

```bash
git add web/src/features/usage-logs/components/dialogs/conversation-dialog.tsx
git commit -m "feat(web): render conversation history using react-json-view with raw text fallback"
```
