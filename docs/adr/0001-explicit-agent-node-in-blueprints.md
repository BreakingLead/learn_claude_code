# Explicit Agent Node in Blueprints

Agent Blueprints will store a node-and-edge graph with an explicit Agent Node as the aggregation point. Capability Nodes such as skills, memory, prompt modules, tool providers, and policies connect into the Agent Node; the resolver derives the runnable agent configuration from those connections. This keeps the graph inspectable in the Web UI, avoids a hidden root concept in the JSON format, and leaves a clean path for future multi-agent workflows where Agent Nodes can become workflow participants.
