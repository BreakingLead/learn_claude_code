# Typed Ports for Blueprint Edges

Agent Builder nodes will expose typed input and output ports, and edges will connect ports rather than carrying their own semantic `kind`. This matches the Blender-style editor model: port types can be color-coded, incompatible ports can be rejected immediately in the UI, and the resolver can derive capability semantics from the connected ports. Keeping type on ports instead of edges avoids duplicating meaning across the node schema and edge schema.
