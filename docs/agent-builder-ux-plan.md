# Agent Builder UX Plan

## Primary User Task

The default Agent Builder path is: create one Agent Blueprint, connect instructions and capabilities to a root Agent node, validate the assembled runtime, and dry-run the result.

## Information Architecture

- `Agents`: single Agent Blueprint editing.
- `Workflows`: multi-agent message/dataflow editing in a separate top-level area.
- `Composites`: reusable assets surfaced in the Agents asset library, with management available outside the primary editing path.

## Agents Page Layout

- Left: Asset Library grouped by user intent: Start, Instructions, Capabilities, Constraints, Logic, Reusable.
- Center: Blueprint canvas with a default root Agent node for new Blueprints.
- Right: contextual Inspector.
- Bottom: JSON Drawer, hidden by default and draggable upward for advanced inspection or manual edits.
- Empty or first-run states should offer lightweight starter actions such as `Add instruction`, `Add tools`, and `Add memory`; avoid a multi-step onboarding wizard.

## Inspector Behavior

- No node selected: Blueprint Inspector with name, description, root agent state, validation summary, dry-run input, and dry-run result.
- Node selected: Node Inspector with label, type, config fields, port explanation, and delete controls.
- `Validate` and `Dry Run` are primary actions because they demonstrate how the agent is assembled. `Save` is secondary.

## Canvas Feedback

- Ports remain typed and color-coded.
- Incompatible connections should fail at the interaction site, not only during full validation.
- Dragging over an incompatible port should visibly reject the connection; failed drops should show a short local error.
