import React, { useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "@xyflow/react/dist/style.css";
import "./main.css";

import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  useEdgesState,
  useNodesState,
} from "@xyflow/react";
import {
  Boxes,
  CheckCircle2,
  ClipboardCheck,
  Copy,
  FileJson,
  GitBranch,
  Layers,
  ListChecks,
  MemoryStick,
  MessageSquareText,
  Play,
  Plus,
  RefreshCw,
  Save,
  ShieldCheck,
  Sparkles,
  Trash2,
  UserRound,
  Wrench,
} from "lucide-react";

const portColors = {
  prompt: "#b57cff",
  toolset: "#5ba8ff",
  memory: "#61d394",
  output: "#aeb7c6",
  message: "#f4b860",
};

const iconSize = 15;

const templateIcons = {
  agent: UserRound,
  prompt: MessageSquareText,
  skill: Sparkles,
  toolset: Wrench,
  memory: MemoryStick,
  policy: ShieldCheck,
  composite: Boxes,
};

function iconElement(Icon, key = "icon") {
  return React.createElement(Icon, {
    "aria-hidden": true,
    className: "button-icon",
    key,
    size: iconSize,
    strokeWidth: 2,
  });
}

function buttonContent(Icon, label) {
  return [
    iconElement(Icon),
    React.createElement("span", { className: "button-label", key: "label" }, label),
  ];
}

function nodeTemplateIcon(template) {
  return templateIcons[template.type] || templateIcons[template.node?.type] || Layers;
}

function BlueprintNode({ data }) {
  const inputs = data.inputs || [];
  const outputs = data.outputs || [];
  return React.createElement("div", { className: "node" }, [
    React.createElement("div", { className: "node-title", key: "title" }, [
      data.label,
      React.createElement("div", { className: "node-type", key: "type" }, data.type),
    ]),
    React.createElement("div", { className: "ports", key: "ports" }, [
      ...inputs.map((port, index) =>
        React.createElement("div", { className: "port", key: `in-${port.id}` }, [
          React.createElement(Handle, {
            key: "handle",
            type: "target",
            id: port.id,
            position: Position.Left,
            style: { background: portColors[port.type] || "#7f8da3" },
          }),
          React.createElement("span", {
            className: "dot",
            key: "dot",
            style: { "--port-color": portColors[port.type] || "#7f8da3" },
          }),
          React.createElement("span", { key: "label" }, port.label || port.id),
        ])
      ),
      ...outputs.map((port, index) =>
        React.createElement("div", { className: "port", key: `out-${port.id}` }, [
          React.createElement(Handle, {
            key: "handle",
            type: "source",
            id: port.id,
            position: Position.Right,
            style: { background: portColors[port.type] || "#7f8da3" },
          }),
          React.createElement("span", {
            className: "dot",
            key: "dot",
            style: { "--port-color": portColors[port.type] || "#7f8da3" },
          }),
          React.createElement("span", { key: "label" }, port.label || port.id),
        ])
      ),
    ]),
  ]);
}

const nodeTypes = { blueprint: BlueprintNode };

function toFlowNodes(blueprint) {
  return (blueprint.nodes || []).map((node) => ({
    id: node.id,
    type: "blueprint",
    position: node.position || { x: 0, y: 0 },
    data: {
      label: node.label || node.id,
      type: node.type,
      inputs: node.inputs,
      outputs: node.outputs,
    },
  }));
}

function toFlowEdges(blueprint) {
  return (blueprint.edges || []).map((edge) => ({
    id: edge.id,
    source: edge.source.node,
    sourceHandle: edge.source.port,
    target: edge.target.node,
    targetHandle: edge.target.port,
    markerEnd: { type: MarkerType.ArrowClosed },
    style: { stroke: "#7f8da3" },
  }));
}

function portFor(blueprint, nodeId, portId, direction) {
  const node = (blueprint.nodes || []).find((item) => item.id === nodeId);
  if (!node) return null;
  const ports = direction === "output" ? node.outputs || [] : node.inputs || [];
  return ports.find((port) => port.id === portId) || null;
}

function connectionError(blueprint, flowEdges, connection) {
  if (!connection.source || !connection.target || !connection.sourceHandle || !connection.targetHandle) {
    return "connection is incomplete";
  }
  const sourcePort = portFor(blueprint, connection.source, connection.sourceHandle, "output");
  const targetPort = portFor(blueprint, connection.target, connection.targetHandle, "input");
  if (!sourcePort) return `source port not found: ${connection.source}.${connection.sourceHandle}`;
  if (!targetPort) return `target port not found: ${connection.target}.${connection.targetHandle}`;
  if (sourcePort.type !== targetPort.type) {
    return `cannot connect ${sourcePort.type} to ${targetPort.type}`;
  }
  const duplicate = flowEdges.some((edge) =>
    edge.source === connection.source &&
    edge.sourceHandle === connection.sourceHandle &&
    edge.target === connection.target &&
    edge.targetHandle === connection.targetHandle
  );
  if (duplicate) return "connection already exists";
  if (!targetPort.multiple) {
    const existing = flowEdges.some((edge) => edge.target === connection.target && edge.targetHandle === connection.targetHandle);
    if (existing) return `${connection.target}.${connection.targetHandle} accepts one connection`;
  }
  return "";
}

function edgeFromConnection(connection) {
  return {
    id: `edge-${connection.source}-${connection.sourceHandle}-${connection.target}-${connection.targetHandle}-${Date.now()}`,
    source: { node: connection.source, port: connection.sourceHandle },
    target: { node: connection.target, port: connection.targetHandle },
  };
}

function withNextPromptInput(blueprint) {
  const edges = blueprint.edges || [];
  let changed = false;
  const nodes = (blueprint.nodes || []).map((node) => {
    if (node.type !== "agent") return node;
    const inputs = node.inputs || [];
    const promptInputs = inputs
      .filter((port) => port.type === "prompt")
      .sort((left, right) => (left.order || 0) - (right.order || 0));
    if (promptInputs.length === 0) return node;
    const hasOpenPromptInput = promptInputs.some((port) =>
      !edges.some((edge) => edge.target.node === node.id && edge.target.port === port.id)
    );
    if (hasOpenPromptInput) return node;
    const nextOrder = Math.max(...promptInputs.map((port) => port.order || 0)) + 1;
    changed = true;
    return {
      ...node,
      inputs: [
        ...inputs,
        {
          id: `prompt_${nextOrder}`,
          type: "prompt",
          label: `Prompt ${nextOrder}`,
          direction: "input",
          order: nextOrder,
        },
      ],
    };
  });
  return changed ? { ...blueprint, nodes } : blueprint;
}

function slug(text) {
  return String(text || "node")
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "") || "node";
}

function nextNodeId(blueprint, prefix) {
  const existing = new Set((blueprint.nodes || []).map((node) => node.id));
  for (let index = 1; index < 10000; index += 1) {
    const id = `${prefix}-${index}`;
    if (!existing.has(id)) return id;
  }
  return `${prefix}-${Date.now()}`;
}

function endpointKey(endpoint) {
  return `${endpoint.node}:${endpoint.port}`;
}

function compositePortQueues(mappings) {
  const result = new Map();
  (mappings || []).forEach((mapping) => {
    const key = endpointKey(mapping.endpoint);
    const ports = result.get(key) || [];
    ports.push(mapping.port.id);
    result.set(key, ports);
  });
  return result;
}

function takeMappedPort(queues, endpoint) {
  const ports = queues.get(endpointKey(endpoint)) || [];
  return ports.length > 1 ? ports.shift() : ports[0];
}

function wrapSelectionWithComposite(blueprint, nodeIDs, composite) {
  const selected = new Set(nodeIDs);
  if (selected.has(blueprint.root_agent)) {
    return { blueprint, error: "root agent cannot be replaced by a composite node" };
  }
  const selectedNodes = (blueprint.nodes || []).filter((node) => selected.has(node.id));
  if (selectedNodes.length === 0) {
    return { blueprint, error: "selection is empty" };
  }
  const minX = Math.min(...selectedNodes.map((node) => node.position?.x || 0));
  const minY = Math.min(...selectedNodes.map((node) => node.position?.y || 0));
  const compositeNodeID = nextNodeId(blueprint, slug(composite.id || "composite"));
  const inputQueues = compositePortQueues(composite.inputs);
  const outputQueues = compositePortQueues(composite.outputs);
  const rewiredEdges = [];
  for (const edge of blueprint.edges || []) {
    const sourceSelected = selected.has(edge.source.node);
    const targetSelected = selected.has(edge.target.node);
    if (sourceSelected && targetSelected) continue;
    if (!sourceSelected && targetSelected) {
      const targetPort = takeMappedPort(inputQueues, edge.target);
      if (!targetPort) return { blueprint, error: `missing composite input for ${endpointKey(edge.target)}` };
      rewiredEdges.push({ ...edge, target: { node: compositeNodeID, port: targetPort } });
      continue;
    }
    if (sourceSelected && !targetSelected) {
      const sourcePort = takeMappedPort(outputQueues, edge.source);
      if (!sourcePort) return { blueprint, error: `missing composite output for ${endpointKey(edge.source)}` };
      rewiredEdges.push({ ...edge, source: { node: compositeNodeID, port: sourcePort } });
      continue;
    }
    rewiredEdges.push(edge);
  }
  const compositeNode = {
    id: compositeNodeID,
    type: "composite",
    label: composite.name || composite.id,
    position: { x: minX, y: minY },
    inputs: (composite.inputs || []).map((mapping) => mapping.port),
    outputs: (composite.outputs || []).map((mapping) => mapping.port),
    config: { definition: composite.id },
  };
  return {
    blueprint: {
      ...blueprint,
      nodes: [
        ...(blueprint.nodes || []).filter((node) => !selected.has(node.id)),
        compositeNode,
      ],
      edges: rewiredEdges,
    },
    nodeID: compositeNodeID,
  };
}

const fallbackTemplates = [
  { type: "agent", label: "Agent", node: { type: "agent", label: "Agent", inputs: [{ id: "prompt_1", type: "prompt", label: "Prompt 1", direction: "input", order: 1 }, { id: "toolset_in", type: "toolset", label: "Tools", direction: "input", multiple: true }, { id: "memory_in", type: "memory", label: "Memory", direction: "input", multiple: true }], outputs: [{ id: "output", type: "output", label: "Output", direction: "output" }], config: { display_name: "Agent" } } },
  { type: "prompt", label: "Prompt", node: { type: "prompt", label: "Prompt", outputs: [{ id: "prompt_out", type: "prompt", label: "Prompt", direction: "output" }], config: { source: "inline", prompt: "Write prompt text here." } } },
  { type: "skill", label: "Skill", node: { type: "skill", label: "Skill", outputs: [{ id: "prompt_out", type: "prompt", label: "Prompt", direction: "output" }], config: { source: "inline", prompt: "Write skill instructions here." } } },
  { type: "toolset", label: "Toolset", node: { type: "toolset", label: "Toolset", outputs: [{ id: "toolset_out", type: "toolset", label: "Toolset", direction: "output" }], config: { tools: ["read_file", "glob"] } } },
  { type: "memory", label: "Memory", node: { type: "memory", label: "Memory", outputs: [{ id: "memory_out", type: "memory", label: "Memory", direction: "output" }], config: { source: "default_memory", path: ".agents/.memory/MEMORY.md" } } },
  { type: "policy", label: "Policy", node: { type: "policy", label: "Policy", inputs: [{ id: "toolset_in", type: "toolset", label: "Tools In", direction: "input", multiple: true }], outputs: [{ id: "toolset_out", type: "toolset", label: "Tools Out", direction: "output" }, { id: "prompt_out", type: "prompt", label: "Prompt", direction: "output" }], config: { allow_tools: [], deny_tools: ["write_file", "edit_file"], prompt: "Follow this policy before using tools." } } },
];

function nodeFromTemplate(template, blueprint) {
  const id = nextNodeId(blueprint, slug(template.type || template.label));
  return {
    ...(template.node || {}),
    id,
    label: template.node?.label || template.label || id,
    position: { x: 120, y: 520 + (blueprint.nodes || []).length * 40 },
    config: { ...(template.node?.config || {}) },
  };
}

function workflowNode(kind, workflow, agentBlueprint) {
  const id = nextNodeId(workflow, kind);
  const base = {
    id,
    position: { x: 160 + (workflow.nodes || []).length * 40, y: 180 },
  };
  if (kind === "workflow-input") {
    return {
      ...base,
      type: "workflow_input",
      label: "Input",
      outputs: [{ id: "message", type: "message", label: "Message", direction: "output" }],
    };
  }
  if (kind === "workflow-output") {
    return {
      ...base,
      type: "workflow_output",
      label: "Output",
      inputs: [{ id: "message", type: "message", label: "Message", direction: "input" }],
    };
  }
  return {
    ...base,
    type: "workflow_agent",
    label: "Agent",
    agent_blueprint: agentBlueprint || "default",
    inputs: [{ id: "input", type: "message", label: "Input", direction: "input" }],
    outputs: [{ id: "output", type: "message", label: "Output", direction: "output" }],
    config: { instruction: "Describe this workflow agent's local responsibility." },
  };
}

function App() {
  const [blueprint, setBlueprint] = useState(null);
  const [source, setSource] = useState("");
  const [status, setStatus] = useState("loading default blueprint");
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [selectedNodeId, setSelectedNodeId] = useState("");
  const [selectedNodeIds, setSelectedNodeIds] = useState([]);
  const selectedNode = (blueprint?.nodes || []).find((node) => node.id === selectedNodeId) || null;
  const [configDraft, setConfigDraft] = useState("");
  const [templates, setTemplates] = useState(fallbackTemplates);
  const [blueprints, setBlueprints] = useState([]);
  const [activeBlueprintId, setActiveBlueprintId] = useState("default");
  const [composites, setComposites] = useState([]);
  const [activeCompositeId, setActiveCompositeId] = useState("");
  const [compositeSource, setCompositeSource] = useState("");
  const [workflows, setWorkflows] = useState([]);
  const [activeWorkflowId, setActiveWorkflowId] = useState("");
  const [workflowSource, setWorkflowSource] = useState("");
  const [workflowPlans, setWorkflowPlans] = useState([]);
  const [activeWorkflowPlanId, setActiveWorkflowPlanId] = useState("");
  const [workflowRuns, setWorkflowRuns] = useState([]);
  const [activeWorkflowRunId, setActiveWorkflowRunId] = useState("");
  const [editorMode, setEditorMode] = useState("blueprint");
  const [validationResult, setValidationResult] = useState(null);
  const [workflowValidationResult, setWorkflowValidationResult] = useState(null);
  const [workflowSimulationResult, setWorkflowSimulationResult] = useState(null);
  const [workflowCompileResult, setWorkflowCompileResult] = useState(null);
  const [workflowRunResult, setWorkflowRunResult] = useState(null);
  const [compiledPlanPath, setCompiledPlanPath] = useState("");
  const [workflowSimulationInput, setWorkflowSimulationInput] = useState("Describe the change you want this workflow to handle.");
  const [workflowExecutionMode, setWorkflowExecutionMode] = useState("dry_run");
  const [workflowExternalCommand, setWorkflowExternalCommand] = useState("./scripts/workflow-dry-invoker");
  const [workflowTimeoutMS, setWorkflowTimeoutMS] = useState(30000);
  const panelRef = useRef(null);
  const [panelInspectorHeight, setPanelInspectorHeight] = useState(260);
  const [panelResizing, setPanelResizing] = useState(false);
  const activeWorkflowPlan = workflowPlans.find((plan) => plan.id === activeWorkflowPlanId) || null;
  const activeWorkflowRun = workflowRuns.find((run) => run.id === activeWorkflowRunId) || null;
  const selectedWorkflowNode = (() => {
    if (editorMode !== "workflow" || !selectedNodeId || !workflowSource.trim()) return null;
    try {
      const workflow = JSON.parse(workflowSource);
      return (workflow.nodes || []).find((node) => node.id === selectedNodeId) || null;
    } catch {
      return null;
    }
  })();

  const updateDocument = useCallback((next, message) => {
    setBlueprint(next);
    setSource(JSON.stringify(next, null, 2));
    setNodes(toFlowNodes(next));
    setEdges(toFlowEdges(next));
    setValidationResult(null);
    if (message) setStatus(message);
  }, [setEdges, setNodes]);

  const setBlueprintDocument = useCallback((next, message) => {
    updateDocument(next, message);
    setSelectedNodeId((id) => id && !(next.nodes || []).some((node) => node.id === id) ? "" : id);
    setSelectedNodeIds((ids) => ids.filter((id) => (next.nodes || []).some((node) => node.id === id)));
  }, [updateDocument]);

  const updateWorkflowDocument = useCallback((next, message) => {
    setWorkflowSource(JSON.stringify(next, null, 2));
    setNodes(toFlowNodes(next));
    setEdges(toFlowEdges(next));
    setValidationResult(null);
    setWorkflowValidationResult(null);
    setWorkflowSimulationResult(null);
    setWorkflowCompileResult(null);
    setWorkflowRunResult(null);
    setWorkflowRuns([]);
    setActiveWorkflowRunId("");
    setCompiledPlanPath("");
    if (message) setStatus(message);
  }, [setEdges, setNodes]);

  const loadBlueprint = useCallback(async (id) => {
    const response = await fetch(`/api/blueprints/${id}`);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setActiveBlueprintId(data.id);
    setBlueprintDocument(data, `loaded ${data.id}`);
  }, [setBlueprintDocument]);

  const loadBlueprints = useCallback(async () => {
    const response = await fetch("/api/blueprints");
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setBlueprints(Array.isArray(data.blueprints) ? data.blueprints : []);
  }, []);

  const loadComposites = useCallback(async () => {
    const response = await fetch("/api/composites");
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setComposites(Array.isArray(data.composites) ? data.composites : []);
  }, []);

  const loadComposite = useCallback(async (id) => {
    const response = await fetch(`/api/composites/${id}`);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setActiveCompositeId(data.id);
    setCompositeSource(JSON.stringify(data, null, 2));
    setEditorMode("composite");
    setStatus(`loaded composite ${data.id}`);
  }, []);

  const loadWorkflows = useCallback(async () => {
    const response = await fetch("/api/workflows");
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setWorkflows(Array.isArray(data.workflows) ? data.workflows : []);
  }, []);

  const loadWorkflowPlans = useCallback(async () => {
    const response = await fetch("/api/workflow-plans");
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    const plans = Array.isArray(data.plans) ? data.plans : [];
    setWorkflowPlans(plans);
    setActiveWorkflowPlanId((id) => id && plans.some((plan) => plan.id === id) ? id : plans[0]?.id || "");
  }, []);

  const loadWorkflowRuns = useCallback(async (workflowId) => {
    if (!workflowId) {
      setWorkflowRuns([]);
      setActiveWorkflowRunId("");
      return;
    }
    const response = await fetch(`/api/workflow-runs/${workflowId}`);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    const runs = Array.isArray(data.runs) ? data.runs : [];
    setWorkflowRuns(runs);
    setActiveWorkflowRunId((id) => id && runs.some((run) => run.id === id) ? id : runs[0]?.id || "");
  }, []);

  const loadWorkflow = useCallback(async (id) => {
    const response = await fetch(`/api/workflows/${id}`);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setActiveWorkflowId(data.id);
    updateWorkflowDocument(data);
    setEditorMode("workflow");
    setStatus(`loaded workflow ${data.id}`);
  }, [updateWorkflowDocument]);

  const loadWorkflowRun = useCallback(async (workflowId, runId) => {
    if (!workflowId || !runId) return;
    const response = await fetch(`/api/workflow-runs/${workflowId}/${runId}`);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    setActiveWorkflowRunId(data.id);
    setWorkflowRunResult({ ok: true, run: data });
    setStatus(`loaded workflow run ${data.id}`);
  }, []);

  const downloadWorkflowRunReport = async () => {
    if (!activeWorkflowPlanId || !activeWorkflowRunId) return;
    try {
      const response = await fetch(`/api/workflow-runs/${activeWorkflowPlanId}/${activeWorkflowRunId}/report`);
      const text = await response.text();
      if (!response.ok) {
        throw new Error(text || response.statusText);
      }
      const blob = new Blob([text], { type: "text/markdown;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `${activeWorkflowRunId}.md`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
      setStatus(`downloaded workflow run report ${activeWorkflowRunId}`);
    } catch (error) {
      setStatus(`workflow run report failed: ${error.message}`);
    }
  };

  const deleteWorkflowRun = async () => {
    if (!activeWorkflowPlanId || !activeWorkflowRunId) return;
    if (!window.confirm(`Delete workflow run ${activeWorkflowRunId}?`)) return;
    try {
      const response = await fetch(`/api/workflow-runs/${activeWorkflowPlanId}/${activeWorkflowRunId}`, { method: "DELETE" });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`workflow run delete failed: ${result.error || response.statusText}`);
        return;
      }
      setWorkflowRunResult(null);
      setActiveWorkflowRunId("");
      await loadWorkflowRuns(activeWorkflowPlanId);
      setStatus("workflow run deleted");
    } catch (error) {
      setStatus(`workflow run delete failed: ${error.message}`);
    }
  };

  const rerunWorkflowRun = async () => {
    if (!activeWorkflowPlanId || !activeWorkflowRunId) return;
    try {
      const response = await fetch(`/api/workflow-runs/${activeWorkflowPlanId}/${activeWorkflowRunId}/rerun`, { method: "POST" });
      const result = await response.json();
      setWorkflowRunResult(result);
      if (!response.ok || !result.ok) {
        if (result.run?.id) {
          setActiveWorkflowRunId(result.run.id);
          await loadWorkflowRuns(result.run.workflow_id || activeWorkflowPlanId);
        }
        setStatus(`workflow run rerun failed: ${result.error || response.statusText}`);
        return;
      }
      setActiveWorkflowRunId(result.run?.id || "");
      await loadWorkflowRuns(result.run?.workflow_id || activeWorkflowPlanId);
      setStatus(`workflow run rerun ready: ${(result.run?.steps || []).length} agent steps`);
    } catch (error) {
      setWorkflowRunResult({ ok: false, error: error.message });
      setStatus(`workflow run rerun failed: ${error.message}`);
    }
  };

  const loadWorkflowPlan = useCallback(async (id) => {
    const response = await fetch(`/api/workflow-plans/${id}`);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || response.statusText);
    const summary = workflowPlans.find((plan) => plan.id === data.workflow_id) || {};
    setActiveWorkflowPlanId(data.workflow_id);
    setWorkflowCompileResult({ ok: true, plan: { ...data, stale: !!summary.stale, current_hash: summary.current_hash || "" } });
    setWorkflowRunResult(null);
    setCompiledPlanPath(summary.path || "");
    await loadWorkflowRuns(data.workflow_id);
    setStatus(`loaded compiled plan ${data.workflow_id}`);
  }, [loadWorkflowRuns, workflowPlans]);

  const deleteWorkflowPlan = async () => {
    if (!activeWorkflowPlanId) return;
    if (!window.confirm(`Delete workflow plan ${activeWorkflowPlanId}?`)) return;
    try {
      const response = await fetch(`/api/workflow-plans/${activeWorkflowPlanId}`, { method: "DELETE" });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`workflow plan delete failed: ${result.error || response.statusText}`);
        return;
      }
      setWorkflowCompileResult(null);
      setWorkflowRunResult(null);
      setCompiledPlanPath("");
      await loadWorkflowPlans();
      setStatus("workflow plan deleted");
    } catch (error) {
      setStatus(`workflow plan delete failed: ${error.message}`);
    }
  };

  const refreshWorkflowPlan = async () => {
    if (!activeWorkflowPlanId) return;
    try {
      const response = await fetch(`/api/workflow-plans/${activeWorkflowPlanId}/refresh`, { method: "POST" });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`workflow plan refresh failed: ${result.error || response.statusText}`);
        return;
      }
      setWorkflowCompileResult({ ok: true, plan: { ...(result.plan || {}), stale: false, current_hash: result.plan?.source_hash || "" } });
      setCompiledPlanPath(result.path || "");
      setActiveWorkflowPlanId(result.plan?.workflow_id || activeWorkflowPlanId);
      await loadWorkflowPlans();
      setStatus(`workflow plan refreshed: ${result.path || "(unknown path)"}`);
    } catch (error) {
      setStatus(`workflow plan refresh failed: ${error.message}`);
    }
  };

  const runWorkflowPlan = async () => {
    if (!activeWorkflowPlanId) return;
    try {
      const response = await fetch(`/api/workflow-plans/${activeWorkflowPlanId}/run`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          input: workflowSimulationInput,
          execution_mode: workflowExecutionMode,
          external_command: workflowExternalCommand.trim() ? workflowExternalCommand.trim().split(/\s+/) : [],
          timeout_ms: Number(workflowTimeoutMS) || 30000,
        }),
      });
      const result = await response.json();
      setWorkflowRunResult(result);
      if (!response.ok || !result.ok) {
        if (result.run?.id) {
          setActiveWorkflowRunId(result.run.id);
          await loadWorkflowRuns(result.run.workflow_id || activeWorkflowPlanId);
        }
        setStatus(`workflow plan run failed: ${result.error || response.statusText}`);
        return;
      }
      setActiveWorkflowRunId(result.run?.id || "");
      await loadWorkflowRuns(result.run?.workflow_id || activeWorkflowPlanId);
      setStatus(`workflow plan run ready: ${(result.run?.steps || []).length} agent steps`);
    } catch (error) {
      setWorkflowRunResult({ ok: false, error: error.message });
      setStatus(`workflow plan run failed: ${error.message}`);
    }
  };

  const loadTemplates = useCallback(async () => {
    try {
      const response = await fetch("/api/node-templates");
      const data = await response.json();
      if (Array.isArray(data.templates) && data.templates.length > 0) {
        setTemplates(data.templates);
      }
    } catch (error) {
      setTemplates(fallbackTemplates);
    }
  }, []);

  useEffect(() => {
    loadBlueprints().catch((error) => setStatus(`list failed: ${error.message}`));
    loadComposites().catch((error) => setStatus(`composite list failed: ${error.message}`));
    loadWorkflows().catch((error) => setStatus(`workflow list failed: ${error.message}`));
    loadWorkflowPlans().catch((error) => setStatus(`workflow plan list failed: ${error.message}`));
    loadBlueprint("default").catch((error) => setStatus(`load failed: ${error.message}`));
  }, [loadBlueprint, loadBlueprints, loadComposites, loadWorkflowPlans, loadWorkflows]);

  useEffect(() => {
    loadTemplates();
  }, [loadTemplates]);

  useEffect(() => {
    if (editorMode === "workflow" && activeWorkflowPlanId) {
      loadWorkflowRuns(activeWorkflowPlanId).catch((error) => setStatus(`workflow run list failed: ${error.message}`));
    }
  }, [activeWorkflowPlanId, editorMode, loadWorkflowRuns]);

  useEffect(() => {
    setConfigDraft(selectedNode ? JSON.stringify(selectedNode.config || {}, null, 2) : "");
  }, [selectedNodeId]);

  useEffect(() => {
    if (!panelResizing) return undefined;
    const move = (event) => {
      const panel = panelRef.current;
      if (!panel) return;
      const rect = panel.getBoundingClientRect();
      const panelbarHeight = panel.querySelector(".panelbar")?.getBoundingClientRect().height || 44;
      const minInspector = 120;
      const minEditor = 160;
      const next = event.clientY - rect.top - panelbarHeight;
      const max = Math.max(minInspector, rect.height - panelbarHeight - minEditor);
      setPanelInspectorHeight(Math.min(Math.max(next, minInspector), max));
    };
    const stop = () => setPanelResizing(false);
    document.body.classList.add("panel-resizing");
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", stop);
    return () => {
      document.body.classList.remove("panel-resizing");
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", stop);
    };
  }, [panelResizing]);

  const validConnection = useCallback((connection) => {
    if (editorMode === "workflow") {
      try {
        return connectionError(JSON.parse(workflowSource), edges, connection) === "";
      } catch {
        return false;
      }
    }
    if (!blueprint) return false;
    return connectionError(blueprint, edges, connection) === "";
  }, [blueprint, edges, editorMode, workflowSource]);

  const parseSource = () => {
    const next = JSON.parse(source);
    setBlueprintDocument(next);
    return next;
  };

  const addNode = useCallback((template) => {
    if (!blueprint) return;
    const node = nodeFromTemplate(template, blueprint);
    const next = {
      ...blueprint,
      nodes: [...(blueprint.nodes || []), node],
    };
    updateDocument(next, `added ${node.label}`);
    setSelectedNodeId(node.id);
  }, [blueprint, updateDocument]);

  const connect = useCallback((connection) => {
    if (editorMode === "workflow") {
      try {
        const workflow = JSON.parse(workflowSource);
        const error = connectionError(workflow, edges, connection);
        if (error) {
          setStatus(`workflow connection rejected: ${error}`);
          return;
        }
        const edge = edgeFromConnection(connection);
        updateWorkflowDocument({
          ...workflow,
          edges: [...(workflow.edges || []), edge],
        }, "workflow connected");
      } catch (error) {
        setStatus(`workflow connection failed: ${error.message}`);
      }
      return;
    }
    if (!blueprint) return;
    const error = connectionError(blueprint, edges, connection);
    if (error) {
      setStatus(`connection rejected: ${error}`);
      return;
    }
    const edge = edgeFromConnection(connection);
    const next = withNextPromptInput({
      ...blueprint,
      edges: [...(blueprint.edges || []), edge],
    });
    updateDocument(next);
    setStatus("connected");
  }, [blueprint, edges, editorMode, updateDocument, updateWorkflowDocument, workflowSource]);

  const updateNodePosition = useCallback((_, node) => {
    if (editorMode === "workflow") {
      try {
        const workflow = JSON.parse(workflowSource);
        const next = {
          ...workflow,
          nodes: (workflow.nodes || []).map((item) =>
            item.id === node.id
              ? { ...item, position: { x: Math.round(node.position.x), y: Math.round(node.position.y) } }
              : item
          ),
        };
        updateWorkflowDocument(next);
      } catch (error) {
        setStatus(`workflow position failed: ${error.message}`);
      }
      return;
    }
    if (!blueprint) return;
    const next = {
      ...blueprint,
      nodes: (blueprint.nodes || []).map((item) =>
        item.id === node.id
          ? { ...item, position: { x: Math.round(node.position.x), y: Math.round(node.position.y) } }
          : item
      ),
    };
    setBlueprint(next);
    setSource(JSON.stringify(next, null, 2));
  }, [blueprint, editorMode, updateWorkflowDocument, workflowSource]);

  const deleteEdges = useCallback((deleted) => {
    if (editorMode === "workflow") {
      try {
        const workflow = JSON.parse(workflowSource);
        const deletedIDs = new Set(deleted.map((edge) => edge.id));
        updateWorkflowDocument({
          ...workflow,
          edges: (workflow.edges || []).filter((edge) => !deletedIDs.has(edge.id)),
        }, "workflow edge removed");
      } catch (error) {
        setStatus(`workflow edge remove failed: ${error.message}`);
      }
      return;
    }
    if (!blueprint || deleted.length === 0) return;
    const deletedIDs = new Set(deleted.map((edge) => edge.id));
    const next = {
      ...blueprint,
      edges: (blueprint.edges || []).filter((edge) => !deletedIDs.has(edge.id)),
    };
    setBlueprint(next);
    setSource(JSON.stringify(next, null, 2));
    setStatus("edge removed");
  }, [blueprint, editorMode, updateWorkflowDocument, workflowSource]);

  const selectNode = useCallback((_, node) => {
    setSelectedNodeId(node.id);
    setSelectedNodeIds([node.id]);
  }, []);

  const selectNodes = useCallback(({ nodes: selected }) => {
    const ids = selected.map((node) => node.id);
    setSelectedNodeIds(ids);
    setSelectedNodeId(ids[0] || "");
  }, []);

  const updateSelectedNode = useCallback((patch) => {
    if (!blueprint || !selectedNode) return;
    const next = {
      ...blueprint,
      nodes: (blueprint.nodes || []).map((node) =>
        node.id === selectedNode.id ? { ...node, ...patch } : node
      ),
    };
    updateDocument(next, "node updated");
  }, [blueprint, selectedNode, updateDocument]);

  const updateSelectedConfig = useCallback((raw) => {
    setConfigDraft(raw);
    if (!selectedNode) return;
    try {
      const config = JSON.parse(raw);
      updateSelectedNode({ config });
    } catch (error) {
      setStatus(`config invalid: ${error.message}`);
    }
  }, [selectedNode, updateSelectedNode]);

  const patchSelectedConfig = useCallback((patch) => {
    if (!selectedNode) return;
    const config = { ...(selectedNode.config || {}), ...patch };
    updateSelectedNode({ config });
    setConfigDraft(JSON.stringify(config, null, 2));
  }, [selectedNode, updateSelectedNode]);

  const configValue = (key) => {
    const value = selectedNode?.config?.[key];
    if (Array.isArray(value)) return value.join(", ");
    return value == null ? "" : String(value);
  };

  const updateWorkflowSource = useCallback((raw) => {
    setWorkflowSource(raw);
    setWorkflowValidationResult(null);
    setWorkflowSimulationResult(null);
    setWorkflowCompileResult(null);
    setCompiledPlanPath("");
    try {
      const next = JSON.parse(raw);
      setNodes(toFlowNodes(next));
      setEdges(toFlowEdges(next));
    } catch {
      // Keep the user's invalid draft in the editor; validation will report the parse error.
    }
  }, [setEdges, setNodes]);

  const addWorkflowNode = useCallback((kind) => {
    try {
      const workflow = JSON.parse(workflowSource);
      const node = workflowNode(kind, workflow, activeBlueprintId || "default");
      updateWorkflowDocument({
        ...workflow,
        nodes: [...(workflow.nodes || []), node],
      }, `added ${node.label}`);
      setSelectedNodeId(node.id);
      setSelectedNodeIds([node.id]);
    } catch (error) {
      setStatus(`workflow node failed: ${error.message}`);
    }
  }, [activeBlueprintId, updateWorkflowDocument, workflowSource]);

  const updateSelectedWorkflowNode = useCallback((patch) => {
    if (!selectedWorkflowNode) return;
    try {
      const workflow = JSON.parse(workflowSource);
      updateWorkflowDocument({
        ...workflow,
        nodes: (workflow.nodes || []).map((node) =>
          node.id === selectedWorkflowNode.id ? { ...node, ...patch } : node
        ),
      }, "workflow node updated");
    } catch (error) {
      setStatus(`workflow node update failed: ${error.message}`);
    }
  }, [selectedWorkflowNode, updateWorkflowDocument, workflowSource]);

  const deleteSelectedWorkflowNode = useCallback(() => {
    if (!selectedWorkflowNode) return;
    try {
      const workflow = JSON.parse(workflowSource);
      updateWorkflowDocument({
        ...workflow,
        nodes: (workflow.nodes || []).filter((node) => node.id !== selectedWorkflowNode.id),
        edges: (workflow.edges || []).filter((edge) =>
          edge.source.node !== selectedWorkflowNode.id && edge.target.node !== selectedWorkflowNode.id
        ),
      }, "workflow node removed");
      setSelectedNodeId("");
      setSelectedNodeIds([]);
    } catch (error) {
      setStatus(`workflow node remove failed: ${error.message}`);
    }
  }, [selectedWorkflowNode, updateWorkflowDocument, workflowSource]);

  const deleteSelectedNode = useCallback(() => {
    if (!blueprint || !selectedNode || selectedNode.id === blueprint.root_agent) return;
    const next = {
      ...blueprint,
      nodes: (blueprint.nodes || []).filter((node) => node.id !== selectedNode.id),
      edges: (blueprint.edges || []).filter((edge) =>
        edge.source.node !== selectedNode.id && edge.target.node !== selectedNode.id
      ),
    };
    updateDocument(next, "node removed");
    setSelectedNodeId("");
    setSelectedNodeIds([]);
  }, [blueprint, selectedNode, updateDocument]);

  const setSelectedAgentAsRoot = useCallback(() => {
    if (!blueprint || !selectedNode || selectedNode.type !== "agent") return;
    updateDocument({ ...blueprint, root_agent: selectedNode.id }, `root agent: ${selectedNode.id}`);
  }, [blueprint, selectedNode, updateDocument]);

  const openBlueprintPanel = useCallback(() => {
    setEditorMode("blueprint");
    if (blueprint) {
      setNodes(toFlowNodes(blueprint));
      setEdges(toFlowEdges(blueprint));
    }
  }, [blueprint, setEdges, setNodes]);

  const createComposite = async () => {
    try {
      const next = parseSource();
      const nodeIDs = selectedNodeIds.length > 0 ? selectedNodeIds : (selectedNodeId ? [selectedNodeId] : []);
      if (nodeIDs.length === 0) {
        setStatus("select nodes before creating a composite");
        return;
      }
      const defaultName = nodeIDs.length === 1
        ? ((next.nodes || []).find((node) => node.id === nodeIDs[0])?.label || "Composite")
        : "Composite";
      const name = window.prompt("Composite name", defaultName);
      if (name === null) return;
      const id = window.prompt("Composite id", slug(name || defaultName));
      if (id === null) return;
      const response = await fetch("/api/composites/from-selection", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ blueprint: next, node_ids: nodeIDs, id, name }),
      });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`composite failed: ${result.error || response.statusText}`);
        return;
      }
      await loadTemplates();
      await loadComposites();
      setActiveCompositeId(result.composite.id);
      setCompositeSource(JSON.stringify(result.composite, null, 2));
      const wrapped = wrapSelectionWithComposite(next, nodeIDs, result.composite);
      if (wrapped.error) {
        setStatus(`created composite: ${result.composite.id}; ${wrapped.error}`);
        return;
      }
      updateDocument(wrapped.blueprint, `created composite: ${result.composite.id}`);
      setSelectedNodeId(wrapped.nodeID);
      setSelectedNodeIds([wrapped.nodeID]);
    } catch (error) {
      setStatus(`composite failed: ${error.message}`);
    }
  };

  const validate = async () => {
    try {
      const next = parseSource();
      const response = await fetch(`/api/blueprints/${next.id}/validate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      setValidationResult(result);
      if (result.ok) {
        const tools = result.capabilities?.tool_names || [];
        const preview = tools.slice(0, 5).join(", ");
        const diagnostics = [
          ...(result.diagnostics || []),
          ...(result.capabilities?.diagnostics || []),
        ];
        const diagText = diagnostics.length > 0 ? `; diagnostics(${diagnostics.length})` : "";
        setStatus(`valid: ${result.resolved.id}; tools(${tools.length}) ${preview}${diagText}`);
      } else {
        setStatus(`invalid: ${result.error}`);
      }
    } catch (error) {
      setStatus(`invalid: ${error.message}`);
    }
  };

  const expandComposites = async () => {
    try {
      const next = parseSource();
      const response = await fetch(`/api/blueprints/${next.id}/validate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      if (!result.ok) {
        setStatus(`expand failed: ${result.error}`);
        return;
      }
      if (!result.expanded) {
        setStatus("expand failed: missing expanded blueprint");
        return;
      }
      const before = (next.nodes || []).filter((node) => node.type === "composite").length;
      const after = (result.expanded.nodes || []).filter((node) => node.type === "composite").length;
      updateDocument(result.expanded, `expanded composites: ${before - after}`);
      setSelectedNodeId("");
      setSelectedNodeIds([]);
    } catch (error) {
      setStatus(`expand failed: ${error.message}`);
    }
  };

  const renderValidationResult = () => {
    if (!validationResult) return null;
    if (!validationResult.ok) {
      return React.createElement("div", { className: "validation", key: "validation" }, [
        React.createElement("div", { className: "validation-title", key: "title" }, "Validation"),
        React.createElement("div", { className: "diagnostics", key: "error" },
          validationResult.error || "Blueprint is invalid."
        ),
      ]);
    }
    const tools = validationResult.capabilities?.tool_names || [];
    const diagnostics = [
      ...(validationResult.diagnostics || []),
      ...(validationResult.capabilities?.diagnostics || []),
    ];
    const policies = validationResult.capabilities?.policies || [];
    const promptBlocks = validationResult.prompt_blocks || [];
    const runtimeCommand = validationResult.runtime?.command || "";
    return React.createElement("div", { className: "validation", key: "validation" }, [
      React.createElement("div", { className: "validation-title", key: "title" },
        `Valid: ${validationResult.resolved?.id || "agent"}`
      ),
      runtimeCommand
        ? React.createElement("div", { className: "field", key: "runtime" }, [
            React.createElement("label", { key: "label" }, "Run"),
            React.createElement("input", { key: "input", value: runtimeCommand, readOnly: true }),
          ])
        : null,
      React.createElement("div", { className: "muted", key: "tools-label" }, `Effective tools (${tools.length})`),
      tools.length > 0
        ? React.createElement("div", { className: "chip-row", key: "tools" },
            tools.map((tool) => React.createElement("span", { className: "chip", key: tool }, tool))
          )
        : React.createElement("div", { className: "muted", key: "no-tools" }, "No tools resolved."),
      policies.length > 0
        ? React.createElement("div", { className: "field", key: "policies" }, [
            React.createElement("label", { key: "label" }, "Policy trace"),
            ...policies.map((policy) => React.createElement("div", { className: "validation", key: policy.node_id }, [
              React.createElement("div", { className: "validation-title", key: "title" }, policy.node_id),
              React.createElement("div", { className: "muted", key: "input" }, `in: ${(policy.input_tools || []).join(", ") || "(none)"}`),
              React.createElement("div", { className: "muted", key: "output" }, `out: ${(policy.output_tools || []).join(", ") || "(none)"}`),
              (policy.dropped_tools || []).length > 0
                ? React.createElement("div", { className: "diagnostics", key: "dropped" }, `dropped: ${policy.dropped_tools.join(", ")}`)
                : React.createElement("div", { className: "muted", key: "dropped" }, "dropped: (none)"),
            ])),
          ])
        : null,
      promptBlocks.length > 0
        ? React.createElement("div", { className: "field", key: "prompt-preview" }, [
            React.createElement("label", { key: "label" }, "Prompt preview"),
            ...promptBlocks.map((block, index) => React.createElement("div", { className: "validation", key: `${block.node_id}-${index}` }, [
              React.createElement("div", { className: "validation-title", key: "title" }, `${index + 1}. ${block.name || block.node_id}`),
              React.createElement("div", { className: "muted", key: "source" }, `source: ${block.source || "(unknown)"}`),
              React.createElement("div", { className: "muted", key: "preview" }, block.preview || "(empty)"),
            ])),
          ])
        : null,
      diagnostics.length > 0
        ? React.createElement("div", { className: "diagnostics", key: "diagnostics" },
            diagnostics.map((item, index) => React.createElement("div", { key: `diagnostic-${index}` }, item))
          )
        : React.createElement("div", { className: "muted", key: "no-diagnostics" }, "No diagnostics."),
    ]);
  };

  const createBlueprint = async (sourceID) => {
    try {
      const defaultName = sourceID ? `${blueprint?.name || sourceID} Copy` : "New Agent";
      const name = window.prompt("Blueprint name", defaultName);
      if (name === null) return;
      const id = window.prompt("Blueprint id", slug(name || defaultName));
      if (id === null) return;
      const response = await fetch("/api/blueprints", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id, name, source_id: sourceID || "" }),
      });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`create failed: ${result.error || response.statusText}`);
        return;
      }
      await loadBlueprints();
      setActiveBlueprintId(result.blueprint.id);
      setBlueprintDocument(result.blueprint, `created ${result.blueprint.id}`);
    } catch (error) {
      setStatus(`create failed: ${error.message}`);
    }
  };

  const createWorkflow = async (sourceID) => {
    try {
      const current = workflowSource.trim() ? JSON.parse(workflowSource) : null;
      const defaultName = sourceID ? `${current?.name || sourceID} Copy` : "New Workflow";
      const name = window.prompt("Workflow name", defaultName);
      if (name === null) return;
      const id = window.prompt("Workflow id", slug(name || defaultName));
      if (id === null) return;
      const response = await fetch("/api/workflows", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id, name, source_id: sourceID || "" }),
      });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`workflow create failed: ${result.error || response.statusText}`);
        return;
      }
      await loadWorkflows();
      setActiveWorkflowId(result.workflow.id);
      updateWorkflowDocument(result.workflow, `created workflow ${result.workflow.id}`);
      setEditorMode("workflow");
    } catch (error) {
      setStatus(`workflow create failed: ${error.message}`);
    }
  };

  const openCompositePanel = async () => {
    if (activeCompositeId) {
      await loadComposite(activeCompositeId);
      return;
    }
    if (composites.length === 0) {
      setStatus("no composites yet");
      setEditorMode("composite");
      return;
    }
    await loadComposite(composites[0].id);
  };

  const openWorkflowPanel = async () => {
    if (activeWorkflowId) {
      await loadWorkflow(activeWorkflowId);
      return;
    }
    if (workflows.length === 0) {
      setStatus("no workflows yet");
      setEditorMode("workflow");
      return;
    }
    await loadWorkflow(workflows[0].id);
  };

  const saveComposite = async () => {
    try {
      const next = JSON.parse(compositeSource);
      const response = await fetch(`/api/composites/${next.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`composite save failed: ${result.error || response.statusText}`);
        return;
      }
      setActiveCompositeId(next.id);
      await loadComposites();
      await loadTemplates();
      setStatus("composite saved");
    } catch (error) {
      setStatus(`composite save failed: ${error.message}`);
    }
  };

  const validateWorkflow = async () => {
    try {
      const next = JSON.parse(workflowSource);
      const response = await fetch(`/api/workflows/${next.id}/validate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      setWorkflowValidationResult(result);
      if (!response.ok || !result.ok) {
        setStatus(`workflow invalid: ${result.error || response.statusText}`);
        return;
      }
      setStatus(`workflow valid: ${(result.order || []).join(" -> ")}`);
    } catch (error) {
      setStatus(`workflow invalid: ${error.message}`);
    }
  };

  const simulateWorkflow = async () => {
    try {
      const next = JSON.parse(workflowSource);
      const response = await fetch(`/api/workflows/${next.id}/simulate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workflow: next, input: workflowSimulationInput }),
      });
      const result = await response.json();
      setWorkflowSimulationResult(result);
      if (!response.ok || !result.ok) {
        setStatus(`workflow simulation failed: ${result.error || response.statusText}`);
        return;
      }
      setStatus(`workflow simulation ready: ${(result.steps || []).length} steps`);
    } catch (error) {
      setStatus(`workflow simulation failed: ${error.message}`);
    }
  };

  const compileWorkflow = async () => {
    try {
      const next = JSON.parse(workflowSource);
      const response = await fetch(`/api/workflows/${next.id}/compile`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      setWorkflowCompileResult(result);
      if (!response.ok || !result.ok) {
        setStatus(`workflow compile failed: ${result.error || response.statusText}`);
        return;
      }
      setStatus(`workflow compiled: ${(result.plan?.agent_runs || []).length} agent runs`);
    } catch (error) {
      setStatus(`workflow compile failed: ${error.message}`);
    }
  };

  const saveCompiledWorkflowPlan = async () => {
    try {
      const next = JSON.parse(workflowSource);
      const response = await fetch(`/api/workflows/${next.id}/compiled-plan`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      setWorkflowCompileResult(result);
      if (!response.ok || !result.ok) {
        setStatus(`compiled plan save failed: ${result.error || response.statusText}`);
        return;
      }
      setCompiledPlanPath(result.path || "");
      setActiveWorkflowPlanId(result.plan?.workflow_id || next.id);
      await loadWorkflowPlans();
      setStatus(`compiled plan saved: ${result.path || "(unknown path)"}`);
    } catch (error) {
      setStatus(`compiled plan save failed: ${error.message}`);
    }
  };

  const renderWorkflowValidationResult = () => {
    if (!workflowValidationResult) return null;
    if (!workflowValidationResult.ok) {
      return React.createElement("div", { className: "validation", key: "workflow-validation" }, [
        React.createElement("div", { className: "validation-title", key: "title" }, "Workflow Validation"),
        React.createElement("div", { className: "diagnostics", key: "error" },
          workflowValidationResult.error || "Workflow is invalid."
        ),
      ]);
    }
    const order = workflowValidationResult.order || [];
    const steps = workflowValidationResult.steps || [];
    const agents = workflowValidationResult.agents || [];
    const diagnostics = workflowValidationResult.diagnostics || [];
    return React.createElement("div", { className: "validation", key: "workflow-validation" }, [
      React.createElement("div", { className: "validation-title", key: "title" }, "Workflow valid"),
      React.createElement("div", { className: "muted", key: "order" }, `order: ${order.join(" -> ") || "(empty)"}`),
      diagnostics.length > 0
        ? React.createElement("div", { className: "diagnostics", key: "diagnostics" },
            diagnostics.map((item) => React.createElement("div", { key: item }, item))
          )
        : null,
      steps.length > 0
        ? React.createElement("div", { className: "field", key: "steps" }, [
            React.createElement("label", { key: "label" }, "Execution plan"),
            ...steps.map((step, index) => React.createElement("div", { className: "validation", key: step.node_id }, [
              React.createElement("div", { className: "validation-title", key: "title" },
                `${index + 1}. ${step.label || step.node_id}`
              ),
              React.createElement("div", { className: "muted", key: "type" }, `type: ${step.type}`),
              step.agent_blueprint
                ? React.createElement("div", { className: "muted", key: "agent" }, `agent: ${step.agent_blueprint}`)
                : null,
              step.instruction
                ? React.createElement("div", { className: "muted", key: "instruction" }, `instruction: ${step.instruction}`)
                : null,
              React.createElement("div", { className: "muted", key: "inputs" },
                `from: ${(step.inputs_from || []).join(", ") || "(start)"}`
              ),
              React.createElement("div", { className: "muted", key: "outputs" },
                `to: ${(step.outputs_to || []).join(", ") || "(end)"}`
              ),
            ])),
          ])
        : null,
      agents.length > 0
        ? React.createElement("div", { className: "field", key: "agents" }, [
            React.createElement("label", { key: "label" }, "Agent bindings"),
            ...agents.map((agent) => React.createElement("div", { className: "validation", key: agent.node_id }, [
              React.createElement("div", { className: "validation-title", key: "title" }, agent.label || agent.node_id),
              React.createElement("div", { className: "muted", key: "blueprint" },
                `${agent.node_id} -> ${agent.blueprint_name ? `${agent.blueprint_name} (${agent.blueprint_id})` : agent.blueprint_id}`
              ),
              (agent.tool_names || []).length > 0
                ? React.createElement("div", { className: "chip-row", key: "tools" },
                    agent.tool_names.map((tool) => React.createElement("span", { className: "chip", key: tool }, tool))
                  )
                : React.createElement("div", { className: "muted", key: "no-tools" }, "No tools resolved."),
              (agent.prompt_blocks || []).length > 0
                ? React.createElement("div", { className: "field", key: "prompts" }, [
                    React.createElement("label", { key: "label" }, "Prompt blocks"),
                    ...agent.prompt_blocks.map((block) =>
                      React.createElement("pre", { className: "code-preview", key: block.node_id },
                        `${block.name} [${block.source}]\n${block.preview || "(empty)"}`
                      )
                    ),
                  ])
                : React.createElement("div", { className: "muted", key: "no-prompts" }, "No prompt blocks resolved."),
              (agent.diagnostics || []).length > 0
                ? React.createElement("div", { className: "diagnostics", key: "diagnostics" },
                    agent.diagnostics.map((item) => React.createElement("div", { key: item }, item))
                  )
                : null,
            ])),
          ])
        : React.createElement("div", { className: "muted", key: "no-agents" }, "No workflow agents."),
    ]);
  };

  const renderWorkflowSimulationResult = () => {
    if (!workflowSimulationResult) return null;
    if (!workflowSimulationResult.ok) {
      return React.createElement("div", { className: "validation", key: "workflow-simulation" }, [
        React.createElement("div", { className: "validation-title", key: "title" }, "Workflow Simulation"),
        React.createElement("div", { className: "diagnostics", key: "error" },
          workflowSimulationResult.error || "Workflow simulation failed."
        ),
      ]);
    }
    const steps = workflowSimulationResult.steps || [];
    return React.createElement("div", { className: "validation", key: "workflow-simulation" }, [
      React.createElement("div", { className: "validation-title", key: "title" }, "Workflow simulation"),
      ...steps.map((step, index) => React.createElement("div", { className: "validation", key: step.node_id }, [
        React.createElement("div", { className: "validation-title", key: "title" },
          `${index + 1}. ${step.label || step.node_id}`
        ),
        React.createElement("div", { className: "muted", key: "type" }, `type: ${step.type}`),
        step.agent_blueprint
          ? React.createElement("div", { className: "muted", key: "agent" }, `agent: ${step.agent_blueprint}`)
          : null,
        step.instruction
          ? React.createElement("div", { className: "muted", key: "instruction" }, `instruction: ${step.instruction}`)
          : null,
        (step.inputs || []).length > 0
          ? React.createElement("div", { className: "field", key: "inputs" }, [
              React.createElement("label", { key: "label" }, "Receives"),
              ...(step.inputs || []).map((input, inputIndex) =>
                React.createElement("pre", { className: "code-preview", key: `${input.from_node}-${inputIndex}` },
                  `${input.from_node}.${input.from_port} -> ${step.node_id}.${input.target_port}\n${input.content || "(empty)"}`
                )
              ),
            ])
          : null,
        (step.outputs || []).length > 0
          ? React.createElement("div", { className: "field", key: "outputs" }, [
              React.createElement("label", { key: "label" }, "Emits"),
              ...(step.outputs || []).map((output) =>
                React.createElement("pre", { className: "code-preview", key: output.port },
                  `${step.node_id}.${output.port} -> ${(output.to || []).map((target) => `${target.node}.${target.port}`).join(", ") || "(end)"}\n${output.content || "(empty)"}`
                )
              ),
            ])
          : null,
      ])),
    ]);
  };

  const renderWorkflowCompileResult = () => {
    if (!workflowCompileResult) return null;
    if (!workflowCompileResult.ok) {
      return React.createElement("div", { className: "validation", key: "workflow-compile" }, [
        React.createElement("div", { className: "validation-title", key: "title" }, "Workflow Compile"),
        React.createElement("div", { className: "diagnostics", key: "error" },
          workflowCompileResult.error || "Workflow compile failed."
        ),
      ]);
    }
    const plan = workflowCompileResult.plan || {};
    const runs = plan.agent_runs || [];
    return React.createElement("div", { className: "validation", key: "workflow-compile" }, [
      React.createElement("div", { className: "validation-title", key: "title" }, "Compiled workflow"),
      compiledPlanPath
        ? React.createElement("div", { className: "muted", key: "path" }, `saved: ${compiledPlanPath}`)
        : null,
      plan.source_hash
        ? React.createElement("div", { className: "muted", key: "source-hash" }, `source hash: ${plan.source_hash.slice(0, 12)}`)
        : null,
      plan.stale
        ? React.createElement("div", { className: "diagnostics", key: "stale" },
            `stale plan: current workflow hash ${(plan.current_hash || "").slice(0, 12) || "(missing)"}`
          )
        : null,
      React.createElement("div", { className: "muted", key: "order" }, `order: ${(plan.order || []).join(" -> ") || "(empty)"}`),
      (plan.diagnostics || []).length > 0
        ? React.createElement("div", { className: "diagnostics", key: "diagnostics" },
            plan.diagnostics.map((item) => React.createElement("div", { key: item }, item))
          )
        : null,
      ...runs.map((run, index) => React.createElement("div", { className: "validation", key: run.node_id }, [
        React.createElement("div", { className: "validation-title", key: "title" },
          `${index + 1}. ${run.label || run.node_id}`
        ),
        React.createElement("div", { className: "muted", key: "blueprint" },
          `blueprint: ${run.blueprint_name ? `${run.blueprint_name} (${run.blueprint_id})` : run.blueprint_id}`
        ),
        run.instruction
          ? React.createElement("div", { className: "muted", key: "instruction" }, `instruction: ${run.instruction}`)
          : null,
        (run.inputs || []).length > 0
          ? React.createElement("pre", { className: "code-preview", key: "inputs" },
              `inputs:\n${(run.inputs || []).map((input) => `- ${input.from_node}.${input.from_port} -> ${run.node_id}.${input.target_port}`).join("\n")}`
            )
          : null,
        (run.outputs || []).length > 0
          ? React.createElement("pre", { className: "code-preview", key: "outputs" },
              `outputs:\n${(run.outputs || []).map((output) => `- ${run.node_id}.${output.port} -> ${(output.to || []).map((target) => `${target.node}.${target.port}`).join(", ") || "(end)"}`).join("\n")}`
            )
          : null,
        (run.tool_names || []).length > 0
          ? React.createElement("div", { className: "chip-row", key: "tools" },
              run.tool_names.map((tool) => React.createElement("span", { className: "chip", key: tool }, tool))
            )
          : null,
        (run.prompt_blocks || []).length > 0
          ? React.createElement("div", { className: "field", key: "prompts" }, [
              React.createElement("label", { key: "label" }, "Prompt blocks"),
              ...run.prompt_blocks.map((block) =>
                React.createElement("pre", { className: "code-preview", key: block.node_id },
                  `${block.name} [${block.source}]\n${block.preview || "(empty)"}`
                )
              ),
            ])
          : null,
      ])),
    ]);
  };

  const formatWorkflowRunOption = (run) => {
    const mode = run.execution_mode || "dry_run";
    const status = run.status || "completed";
    const steps = run.step_count !== undefined ? ` steps=${run.step_count}` : "";
    const failed = run.failed_step_label || run.failed_step_id;
    const failure = failed ? ` failed=${failed}` : "";
    const rerun = run.rerun_of ? ` rerun=${run.rerun_of}` : "";
    const duration = run.duration_ms !== undefined ? ` ${run.duration_ms}ms` : "";
    const stale = run.stale ? " stale" : "";
    return `${run.created_at || run.id} [${mode}:${status}]${steps}${failure}${rerun}${duration}${stale}`;
  };

  const renderWorkflowRunResult = () => {
    if (!workflowRunResult) return null;
    if (!workflowRunResult.ok && !workflowRunResult.run) {
      return React.createElement("div", { className: "validation", key: "workflow-run" }, [
        React.createElement("div", { className: "validation-title", key: "title" }, "Workflow Plan Run"),
        React.createElement("div", { className: "diagnostics", key: "error" },
          workflowRunResult.error || "Workflow plan run failed."
        ),
      ]);
    }
    const run = workflowRunResult.run || {};
    return React.createElement("div", { className: "validation", key: "workflow-run" }, [
      React.createElement("div", { className: "validation-title", key: "title" }, "Workflow plan run"),
      React.createElement("div", { className: run.status === "failed" ? "diagnostics" : "muted", key: "status" },
        `status: ${run.status || (workflowRunResult.ok ? "completed" : "failed")}`
      ),
      !workflowRunResult.ok || run.error
        ? React.createElement("div", { className: "diagnostics", key: "run-error" },
            run.error || workflowRunResult.error || "Workflow plan run failed."
          )
        : null,
      React.createElement("div", { className: "muted", key: "mode" }, `mode: ${run.execution_mode || "dry_run"}`),
      run.rerun_of
        ? React.createElement("div", { className: "muted", key: "rerun-of" }, `rerun of: ${run.rerun_of}`)
        : null,
      (run.external_command || []).length > 0
        ? React.createElement("div", { className: "muted", key: "external-command" },
            `external command: ${(run.external_command || []).join(" ")}`
          )
        : null,
      React.createElement("div", { className: "muted", key: "timeout" }, `timeout: ${run.timeout_ms || 30000}ms`),
      run.duration_ms !== undefined
        ? React.createElement("div", { className: "muted", key: "duration" }, `duration: ${run.duration_ms}ms`)
        : null,
      React.createElement("div", { className: "muted", key: "input" }, `input: ${run.input || "(empty)"}`),
      run.stale
        ? React.createElement("div", { className: "diagnostics", key: "stale" },
            `stale plan: current workflow hash ${(run.current_hash || "").slice(0, 12) || "(missing)"}`
          )
        : null,
      run.plan_snapshot
        ? React.createElement("div", { className: "muted", key: "plan-snapshot" },
            `plan snapshot: ${(run.plan_snapshot.agent_runs || []).length} agents, source ${(run.plan_snapshot.source_hash || "").slice(0, 12) || "(missing)"}`
          )
        : null,
      (run.diagnostics || []).length > 0
        ? React.createElement("div", { className: "diagnostics", key: "diagnostics" },
            run.diagnostics.map((item) => React.createElement("div", { key: item }, item))
          )
        : null,
      ...(run.steps || []).map((step, index) => React.createElement("div", { className: "validation", key: step.node_id }, [
        React.createElement("div", { className: "validation-title", key: "title" },
          `${index + 1}. ${step.label || step.node_id}`
        ),
        React.createElement("div", { className: "muted", key: "blueprint" }, `blueprint: ${step.blueprint_id || "(none)"}`),
        React.createElement("div", { className: step.status === "failed" ? "diagnostics" : "muted", key: "status" },
          `status: ${step.status || "completed"}`
        ),
        step.error
          ? React.createElement("div", { className: "diagnostics", key: "error" }, step.error)
          : null,
        step.duration_ms !== undefined
          ? React.createElement("div", { className: "muted", key: "duration" }, `duration: ${step.duration_ms}ms`)
          : null,
        (step.inputs || []).length > 0
          ? React.createElement("pre", { className: "code-preview", key: "inputs" },
              `receives:\n${(step.inputs || []).map((input) => `${input.from_node}.${input.from_port} -> ${step.node_id}.${input.target_port}\n${input.content || "(empty)"}`).join("\n\n")}`
            )
          : null,
        (step.outputs || []).length > 0
          ? React.createElement("pre", { className: "code-preview", key: "outputs" },
              `emits:\n${(step.outputs || []).map((output) => `${step.node_id}.${output.port} -> ${(output.to || []).map((target) => `${target.node}.${target.port}`).join(", ") || "(end)"}\n${output.content || "(empty)"}`).join("\n\n")}`
            )
          : null,
      ])),
      (run.outputs || []).length > 0
        ? React.createElement("div", { className: "field", key: "outputs" }, [
            React.createElement("label", { key: "label" }, "Final outputs"),
            ...(run.outputs || []).map((output) =>
              React.createElement("pre", { className: "code-preview", key: output.node_id },
                `${output.label || output.node_id}\n${output.content || "(empty)"}`
              )
            ),
          ])
        : null,
    ]);
  };

  const saveWorkflow = async () => {
    try {
      const next = JSON.parse(workflowSource);
      const response = await fetch(`/api/workflows/${next.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`workflow save failed: ${result.error || response.statusText}`);
        return;
      }
      setActiveWorkflowId(next.id);
      await loadWorkflows();
      setStatus("workflow saved");
    } catch (error) {
      setStatus(`workflow save failed: ${error.message}`);
    }
  };

  const deleteWorkflow = async () => {
    if (!activeWorkflowId) return;
    if (!window.confirm(`Delete workflow ${activeWorkflowId}?`)) return;
    try {
      const response = await fetch(`/api/workflows/${activeWorkflowId}`, { method: "DELETE" });
      const result = await response.json();
      if (!response.ok || !result.ok) {
        setStatus(`workflow delete failed: ${result.error || response.statusText}`);
        return;
      }
      const remaining = workflows.filter((item) => item.id !== activeWorkflowId);
      await loadWorkflows();
      setWorkflowValidationResult(null);
      setSelectedNodeId("");
      setSelectedNodeIds([]);
      if (remaining.length > 0) {
        await loadWorkflow(remaining[0].id);
      } else {
        setActiveWorkflowId("");
        setWorkflowSource("");
        setNodes([]);
        setEdges([]);
        setStatus("workflow deleted");
      }
    } catch (error) {
      setStatus(`workflow delete failed: ${error.message}`);
    }
  };

  const save = async () => {
    try {
      const next = parseSource();
      const response = await fetch(`/api/blueprints/${next.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      });
      const result = await response.json();
      if (result.ok) {
        setActiveBlueprintId(next.id);
        await loadBlueprints();
        setStatus("saved");
      } else {
        setStatus(`save failed: ${result.error}`);
      }
    } catch (error) {
      setStatus(`save failed: ${error.message}`);
    }
  };

  return React.createElement("div", { className: "app" }, [
    React.createElement("div", { className: "topbar", key: "topbar" }, [
      React.createElement("div", { className: "brand", key: "brand" }, "Bee Agent Builder"),
      React.createElement("select", {
        key: "blueprint-select",
        value: activeBlueprintId,
        onChange: (event) => loadBlueprint(event.target.value).catch((error) => setStatus(`load failed: ${error.message}`)),
      }, blueprints.map((item) =>
        React.createElement("option", { key: item.id, value: item.id }, item.name ? `${item.name} (${item.id})` : item.id)
      )),
      React.createElement("div", { className: "actions", key: "blueprint-actions" }, [
        React.createElement("button", { key: "new", onClick: () => createBlueprint("") }, buttonContent(Plus, "New")),
        React.createElement("button", {
          key: "duplicate",
          disabled: !activeBlueprintId,
          onClick: () => createBlueprint(activeBlueprintId),
        }, buttonContent(Copy, "Duplicate")),
      ]),
      React.createElement("div", { className: "status", key: "status" }, status),
    ]),
    React.createElement("div", { className: "layout", key: "layout" }, [
      React.createElement("div", { className: "graph", key: "graph" }, [
        React.createElement("div", { className: "node-palette", key: "palette" }, [
          React.createElement("div", { className: "node-palette-title", key: "title" }, "Nodes"),
          ...templates.map((template) =>
            React.createElement("button", {
              className: "node-palette-button",
              key: `add-${template.type}-${template.node?.config?.definition || template.label}`,
              title: template.description || template.label,
              onClick: () => addNode(template),
            }, buttonContent(nodeTemplateIcon(template), template.label))
          ),
        ]),
        React.createElement(ReactFlow, {
            nodes,
            edges,
            nodeTypes,
            onNodesChange,
            onEdgesChange,
            onConnect: connect,
            onEdgesDelete: deleteEdges,
            onNodeClick: selectNode,
            onSelectionChange: selectNodes,
            onPaneClick: () => {
              setSelectedNodeId("");
              setSelectedNodeIds([]);
            },
            onNodeDragStop: updateNodePosition,
            isValidConnection: validConnection,
            fitView: true,
          }, [
            React.createElement(Background, { key: "background", color: "#303746" }),
            React.createElement(Controls, { key: "controls" }),
          ]
        ),
      ]),
      React.createElement("div", {
        className: "panel",
        key: "panel",
        ref: panelRef,
        style: { "--inspector-height": `${panelInspectorHeight}px` },
      }, [
        React.createElement("div", { className: "panelbar", key: "panelbar" }, [
          React.createElement("span", { className: "panel-title", key: "label" },
            editorMode === "blueprint" ? "Blueprint JSON" : editorMode === "workflow" ? "Workflow JSON" : "Composite JSON"
          ),
          React.createElement("div", { className: "actions", key: "actions" }, [
            React.createElement("button", {
              key: "blueprint-mode",
              disabled: editorMode === "blueprint",
              onClick: openBlueprintPanel,
            }, buttonContent(FileJson, "Blueprint")),
            React.createElement("button", {
              key: "composite-mode",
              disabled: editorMode === "composite" && composites.length === 0,
              onClick: () => openCompositePanel().catch((error) => setStatus(`composite load failed: ${error.message}`)),
            }, buttonContent(Boxes, "Composites")),
            React.createElement("button", {
              key: "workflow-mode",
              disabled: editorMode === "workflow" && workflows.length === 0,
              onClick: () => openWorkflowPanel().catch((error) => setStatus(`workflow load failed: ${error.message}`)),
            }, buttonContent(GitBranch, "Workflows")),
            editorMode === "composite" && composites.length > 0
              ? React.createElement("select", {
                  key: "composite-select",
                  value: activeCompositeId,
                  onChange: (event) => loadComposite(event.target.value).catch((error) => setStatus(`composite load failed: ${error.message}`)),
                }, composites.map((item) =>
                  React.createElement("option", { key: item.id, value: item.id }, item.name ? `${item.name} (${item.id})` : item.id)
                ))
              : null,
            editorMode === "workflow" && workflows.length > 0
              ? React.createElement("select", {
                  key: "workflow-select",
                  value: activeWorkflowId,
                  onChange: (event) => loadWorkflow(event.target.value).catch((error) => setStatus(`workflow load failed: ${error.message}`)),
                }, workflows.map((item) =>
                  React.createElement("option", { key: item.id, value: item.id }, item.name ? `${item.name} (${item.id})` : item.id)
                ))
              : null,
            editorMode === "workflow" && workflowPlans.length > 0
              ? React.createElement("select", {
                  key: "workflow-plan-select",
                  value: activeWorkflowPlanId,
                  onChange: (event) => setActiveWorkflowPlanId(event.target.value),
                }, workflowPlans.map((item) =>
                  React.createElement("option", { key: item.id, value: item.id },
                    `${item.name ? `${item.name} plan (${item.id})` : item.id}${item.stale ? " stale" : ""}`
                  )
                ))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "load-workflow-plan",
                  title: activeWorkflowPlan?.stale ? "Saved plan is older than the current workflow JSON." : "Load saved compiled workflow plan.",
                  disabled: !activeWorkflowPlanId,
                  onClick: () => loadWorkflowPlan(activeWorkflowPlanId).catch((error) => setStatus(`workflow plan load failed: ${error.message}`)),
                }, buttonContent(ClipboardCheck, "Load Plan"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "refresh-workflow-plan",
                  disabled: !activeWorkflowPlanId,
                  onClick: refreshWorkflowPlan,
                }, buttonContent(RefreshCw, "Refresh Plan"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "run-workflow-plan",
                  disabled: !activeWorkflowPlanId || (workflowExecutionMode === "external_command" && !workflowExternalCommand.trim()),
                  onClick: runWorkflowPlan,
                }, buttonContent(Play, "Run Plan"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "delete-workflow-plan",
                  disabled: !activeWorkflowPlanId,
                  onClick: deleteWorkflowPlan,
                }, buttonContent(Trash2, "Delete Plan"))
              : null,
            editorMode === "blueprint"
              ? React.createElement("button", {
                  key: "composite",
                  disabled: selectedNodeIds.length === 0 && !selectedNodeId,
                  onClick: createComposite,
                }, buttonContent(Boxes, "Create Composite"))
              : null,
            editorMode === "blueprint"
              ? React.createElement("button", { key: "expand", onClick: expandComposites }, buttonContent(Layers, "Expand Composites"))
              : null,
            editorMode === "blueprint"
              ? React.createElement("button", { key: "validate", onClick: validate }, buttonContent(CheckCircle2, "Validate"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "validate-workflow",
                  disabled: !workflowSource.trim(),
                  onClick: validateWorkflow,
                }, buttonContent(CheckCircle2, "Validate Workflow"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "simulate-workflow",
                  disabled: !workflowSource.trim(),
                  onClick: simulateWorkflow,
                }, buttonContent(Play, "Simulate Workflow"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "compile-workflow",
                  disabled: !workflowSource.trim(),
                  onClick: compileWorkflow,
                }, buttonContent(ListChecks, "Compile Workflow"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "save-compiled-workflow",
                  disabled: !workflowSource.trim(),
                  onClick: saveCompiledWorkflowPlan,
                }, buttonContent(Save, "Save Plan"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "new-workflow",
                  onClick: () => createWorkflow(""),
                }, buttonContent(Plus, "New Workflow"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "duplicate-workflow",
                  disabled: !activeWorkflowId,
                  onClick: () => createWorkflow(activeWorkflowId),
                }, buttonContent(Copy, "Duplicate Workflow"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "delete-workflow",
                  disabled: !activeWorkflowId,
                  onClick: deleteWorkflow,
                }, buttonContent(Trash2, "Delete Workflow"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "add-workflow-input",
                  disabled: !workflowSource.trim(),
                  onClick: () => addWorkflowNode("workflow-input"),
                }, buttonContent(MessageSquareText, "Input"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "add-workflow-agent",
                  disabled: !workflowSource.trim(),
                  onClick: () => addWorkflowNode("workflow-agent"),
                }, buttonContent(UserRound, "Agent"))
              : null,
            editorMode === "workflow"
              ? React.createElement("button", {
                  key: "add-workflow-output",
                  disabled: !workflowSource.trim(),
                  onClick: () => addWorkflowNode("workflow-output"),
                }, buttonContent(FileJson, "Output"))
              : null,
            editorMode === "blueprint"
              ? React.createElement("button", { key: "save", onClick: save }, buttonContent(Save, "Save"))
              : editorMode === "workflow"
              ? React.createElement("button", {
                  key: "save-workflow",
                  disabled: !workflowSource.trim(),
                  onClick: saveWorkflow,
                }, buttonContent(Save, "Save Workflow"))
              : React.createElement("button", {
                  key: "save-composite",
                  disabled: !compositeSource.trim(),
                  onClick: saveComposite,
                }, buttonContent(Save, "Save Composite")),
          ]),
        ]),
        editorMode === "blueprint"
          ? React.createElement("div", { className: "inspector", key: "inspector" }, [
              renderValidationResult(),
              ...(selectedNode
          ? [
              React.createElement("div", { className: "field", key: "id" }, [
                React.createElement("label", { key: "label" }, "Node"),
                React.createElement("input", { key: "input", value: selectedNode.id, readOnly: true }),
              ]),
              React.createElement("div", { className: "field", key: "label-field" }, [
                React.createElement("label", { key: "label" }, "Label"),
                React.createElement("input", {
                  key: "input",
                  value: selectedNode.label || "",
                  onChange: (event) => updateSelectedNode({ label: event.target.value }),
                }),
              ]),
              selectedNode.type === "agent"
                ? React.createElement("div", { className: "field", key: "root-agent" }, [
                    React.createElement("label", { key: "label" }, "Root Agent"),
                    React.createElement("div", { className: "actions", key: "actions" }, [
                      React.createElement("span", { className: "muted", key: "state" },
                        blueprint.root_agent === selectedNode.id ? "Current root" : blueprint.root_agent || "(none)"
                      ),
                      React.createElement("button", {
                        key: "set-root",
                        disabled: blueprint.root_agent === selectedNode.id,
                        onClick: setSelectedAgentAsRoot,
                      }, buttonContent(UserRound, "Set Root")),
                    ]),
                  ])
                : null,
              selectedNode.config && Object.prototype.hasOwnProperty.call(selectedNode.config, "source")
                ? React.createElement("div", { className: "field", key: "source-field" }, [
                    React.createElement("label", { key: "label" }, "Source"),
                    React.createElement("input", {
                      key: "input",
                      value: configValue("source"),
                      placeholder: "inline | skill_file | project_files | active_mode",
                      onChange: (event) => patchSelectedConfig({ source: event.target.value }),
                    }),
                  ])
                : null,
              selectedNode.config && Object.prototype.hasOwnProperty.call(selectedNode.config, "path")
                ? React.createElement("div", { className: "field", key: "path-field" }, [
                    React.createElement("label", { key: "label" }, "Path"),
                    React.createElement("input", {
                      key: "input",
                      value: configValue("path"),
                      placeholder: ".agents/skills/name/SKILL.md",
                      onChange: (event) => patchSelectedConfig({ path: event.target.value }),
                    }),
                  ])
                : null,
              selectedNode.config && Object.prototype.hasOwnProperty.call(selectedNode.config, "prompt")
                ? React.createElement("div", { className: "field", key: "prompt-field" }, [
                    React.createElement("label", { key: "label" }, "Prompt"),
                    React.createElement("textarea", {
                      className: "config-editor",
                      key: "textarea",
                      value: configValue("prompt"),
                      spellCheck: "false",
                      onChange: (event) => patchSelectedConfig({ prompt: event.target.value }),
                    }),
                  ])
                : null,
              selectedNode.config && Object.prototype.hasOwnProperty.call(selectedNode.config, "tools")
                ? React.createElement("div", { className: "field", key: "tools-field" }, [
                    React.createElement("label", { key: "label" }, "Tools"),
                    React.createElement("input", {
                      key: "input",
                      value: configValue("tools"),
                      placeholder: "read_file, glob",
                      onChange: (event) => patchSelectedConfig({
                        tools: event.target.value.split(",").map((item) => item.trim()).filter(Boolean),
                      }),
                    }),
                  ])
                : null,
              selectedNode.config && Object.prototype.hasOwnProperty.call(selectedNode.config, "allow_tools")
                ? React.createElement("div", { className: "field", key: "allow-tools-field" }, [
                    React.createElement("label", { key: "label" }, "Allow Tools"),
                    React.createElement("input", {
                      key: "input",
                      value: configValue("allow_tools"),
                      placeholder: "leave empty to allow all upstream tools",
                      onChange: (event) => patchSelectedConfig({
                        allow_tools: event.target.value.split(",").map((item) => item.trim()).filter(Boolean),
                      }),
                    }),
                  ])
                : null,
              selectedNode.config && Object.prototype.hasOwnProperty.call(selectedNode.config, "deny_tools")
                ? React.createElement("div", { className: "field", key: "deny-tools-field" }, [
                    React.createElement("label", { key: "label" }, "Deny Tools"),
                    React.createElement("input", {
                      key: "input",
                      value: configValue("deny_tools"),
                      placeholder: "write_file, edit_file",
                      onChange: (event) => patchSelectedConfig({
                        deny_tools: event.target.value.split(",").map((item) => item.trim()).filter(Boolean),
                      }),
                    }),
                  ])
                : null,
              React.createElement("div", { className: "field", key: "config" }, [
                React.createElement("label", { key: "label" }, "Config JSON"),
                React.createElement("textarea", {
                  className: "config-editor",
                  key: "textarea",
                  value: configDraft,
                  spellCheck: "false",
                  onChange: (event) => updateSelectedConfig(event.target.value),
                }),
              ]),
              selectedNode.id === blueprint.root_agent
                ? React.createElement("div", { className: "muted", key: "agent-note" }, "Root agent cannot be deleted.")
                : React.createElement("button", { key: "delete", onClick: deleteSelectedNode }, buttonContent(Trash2, "Delete Node")),
            ]
          : [React.createElement("div", { className: "muted", key: "empty-selection" }, "Select a node to edit its label and config.")])
            ])
          : editorMode === "workflow"
          ? React.createElement("div", { className: "inspector", key: "workflow-inspector" }, [
              renderWorkflowValidationResult(),
              React.createElement("div", { className: "field", key: "workflow-runs" }, [
                React.createElement("label", { key: "label" }, "Run History"),
                React.createElement("select", {
                  key: "select",
                  value: activeWorkflowRunId,
                  onChange: (event) => setActiveWorkflowRunId(event.target.value),
                }, [
                  React.createElement("option", { key: "empty", value: "" }, workflowRuns.length ? "Select run" : "No runs"),
                  ...workflowRuns.map((run) =>
                    React.createElement("option", {
                      key: run.id,
                      value: run.id,
                      title: [run.failed_step_error || run.error || "", (run.external_command || []).join(" ")]
                        .filter(Boolean)
                        .join("\n"),
                    },
                      formatWorkflowRunOption(run)
                    )
                  ),
                ]),
                React.createElement("button", {
                  key: "load",
                  disabled: !activeWorkflowPlanId || !activeWorkflowRunId,
                  title: activeWorkflowRun?.output || "Load saved workflow run.",
                  onClick: () => loadWorkflowRun(activeWorkflowPlanId, activeWorkflowRunId).catch((error) => setStatus(`workflow run load failed: ${error.message}`)),
                }, buttonContent(ClipboardCheck, "Load Run")),
                React.createElement("button", {
                  key: "rerun",
                  disabled: !activeWorkflowPlanId || !activeWorkflowRunId,
                  title: "Run the current saved plan with this run's saved input and execution settings.",
                  onClick: rerunWorkflowRun,
                }, buttonContent(RefreshCw, "Rerun")),
                React.createElement("button", {
                  key: "report",
                  disabled: !activeWorkflowPlanId || !activeWorkflowRunId,
                  title: "Download this workflow run as a Markdown evidence report.",
                  onClick: downloadWorkflowRunReport,
                }, buttonContent(FileJson, "Download Report")),
                React.createElement("button", {
                  key: "delete",
                  disabled: !activeWorkflowPlanId || !activeWorkflowRunId,
                  title: "Delete saved workflow run.",
                  onClick: deleteWorkflowRun,
                }, buttonContent(Trash2, "Delete Run")),
              ]),
              React.createElement("div", { className: "field", key: "simulation-input" }, [
                React.createElement("label", { key: "label" }, "Simulation Input"),
                React.createElement("textarea", {
                  className: "config-editor",
                  key: "textarea",
                  value: workflowSimulationInput,
                  spellCheck: "false",
                  onChange: (event) => setWorkflowSimulationInput(event.target.value),
                }),
              ]),
              React.createElement("div", { className: "field", key: "execution-mode" }, [
                React.createElement("label", { key: "label" }, "Execution Mode"),
                React.createElement("select", {
                  key: "select",
                  value: workflowExecutionMode,
                  onChange: (event) => setWorkflowExecutionMode(event.target.value),
                }, [
                  React.createElement("option", { key: "dry-run", value: "dry_run" }, "dry_run"),
                  React.createElement("option", { key: "external-command", value: "external_command" }, "external_command"),
                ]),
              ]),
              React.createElement("div", { className: "field", key: "execution-timeout" }, [
                React.createElement("label", { key: "label" }, "Timeout Ms"),
                React.createElement("input", {
                  key: "input",
                  type: "number",
                  min: "100",
                  step: "100",
                  value: workflowTimeoutMS,
                  onChange: (event) => setWorkflowTimeoutMS(event.target.value),
                }),
              ]),
              workflowExecutionMode === "external_command"
                ? React.createElement("div", { className: "field", key: "external-command" }, [
                    React.createElement("label", { key: "label" }, "External Command"),
                    React.createElement("input", {
                      key: "input",
                      value: workflowExternalCommand,
                      spellCheck: "false",
                      placeholder: "./scripts/workflow-agent-invoker",
                      onChange: (event) => setWorkflowExternalCommand(event.target.value),
                    }),
                  ])
                : null,
              renderWorkflowSimulationResult(),
              renderWorkflowCompileResult(),
              renderWorkflowRunResult(),
              React.createElement("div", { className: "muted", key: "help" }, activeWorkflowId
                ? "Edit the multi-agent workflow DAG JSON. Validation checks typed message ports and cycle-free execution order."
                : "No workflow selected."),
              ...(selectedWorkflowNode
                ? [
                    React.createElement("div", { className: "field", key: "workflow-node-id" }, [
                      React.createElement("label", { key: "label" }, "Workflow Node"),
                      React.createElement("input", { key: "input", value: selectedWorkflowNode.id, readOnly: true }),
                    ]),
                    React.createElement("div", { className: "field", key: "workflow-node-label" }, [
                      React.createElement("label", { key: "label" }, "Label"),
                      React.createElement("input", {
                        key: "input",
                        value: selectedWorkflowNode.label || "",
                        onChange: (event) => updateSelectedWorkflowNode({ label: event.target.value }),
                      }),
                    ]),
                    selectedWorkflowNode.type === "workflow_agent"
                      ? React.createElement("div", { className: "field", key: "workflow-agent-blueprint" }, [
                          React.createElement("label", { key: "label" }, "Agent Blueprint"),
                          React.createElement("select", {
                            key: "select",
                            value: selectedWorkflowNode.agent_blueprint || "default",
                            onChange: (event) => updateSelectedWorkflowNode({ agent_blueprint: event.target.value }),
                          }, blueprints.map((item) =>
                            React.createElement("option", { key: item.id, value: item.id }, item.name ? `${item.name} (${item.id})` : item.id)
                          )),
                        ])
                      : null,
                    selectedWorkflowNode.type === "workflow_agent"
                      ? React.createElement("div", { className: "field", key: "workflow-agent-instruction" }, [
                          React.createElement("label", { key: "label" }, "Instruction"),
                          React.createElement("textarea", {
                            className: "config-editor",
                            key: "textarea",
                            value: selectedWorkflowNode.config?.instruction || "",
                            spellCheck: "false",
                            onChange: (event) => updateSelectedWorkflowNode({
                              config: { ...(selectedWorkflowNode.config || {}), instruction: event.target.value },
                            }),
                          }),
                        ])
                      : null,
                    React.createElement("button", {
                      key: "delete-workflow-node",
                      onClick: deleteSelectedWorkflowNode,
                    }, buttonContent(Trash2, "Delete Workflow Node")),
                  ]
                : [React.createElement("div", { className: "muted", key: "empty-workflow-selection" }, "Select a workflow node to edit it.")]),
            ])
          : React.createElement("div", { className: "inspector", key: "composite-inspector" },
              React.createElement("div", { className: "muted" }, activeCompositeId
                ? "Edit the reusable composite definition directly. Saving refreshes node templates."
                : "No composite selected.")
            ),
        React.createElement("div", {
          className: "panel-resizer",
          key: "panel-resizer",
          role: "separator",
          "aria-orientation": "horizontal",
          title: "Drag to resize inspector",
          onMouseDown: (event) => {
            event.preventDefault();
            setPanelResizing(true);
          },
        }),
        editorMode === "blueprint"
            ? React.createElement("textarea", {
              className: "json-editor",
              key: "editor",
              value: source,
              spellCheck: "false",
              onChange: (event) => {
                setSource(event.target.value);
                setValidationResult(null);
              },
            })
          : editorMode === "workflow"
          ? React.createElement("textarea", {
              className: "json-editor",
              key: "workflow-editor",
              value: workflowSource,
              spellCheck: "false",
              onChange: (event) => updateWorkflowSource(event.target.value),
            })
          : React.createElement("textarea", {
              className: "json-editor",
              key: "composite-editor",
              value: compositeSource,
              spellCheck: "false",
              onChange: (event) => setCompositeSource(event.target.value),
            }),
      ]),
    ]),
  ]);
}

createRoot(document.getElementById("root")).render(React.createElement(App));
