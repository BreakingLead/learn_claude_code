# Root Agent Resolution for Blueprints

Agent Blueprint JSON may contain multiple Agent Nodes, but the first runtime resolver will execute only the Agent Node referenced by `root_agent`. This keeps the schema open for visual prototyping and future multi-agent workflows while keeping the initial runtime path simple: validate the graph, collect Capability Nodes connected to the root Agent Node, and derive one runnable agent configuration.
