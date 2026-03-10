# VXD Training Documentation

Welcome to the VXD (Vortex Dispatch) training guides. Whether you're a first-time user or a contributor looking to extend the system, start here.

## For Users

| Guide | Description |
|-------|-------------|
| [Getting Started](getting-started.md) | Prerequisites, installation, and your first `vxd init` |
| [Tutorial: Your First Requirement](tutorial.md) | Hands-on walkthrough — submit a requirement and watch the full pipeline |
| [Pipeline Workflows](workflows.md) | How stories flow from planning through merge, stage by stage |
| [Configuration Reference](configuration.md) | Every config knob explained with defaults and tuning advice |
| [Agents and Roles](agents-and-roles.md) | Role hierarchy, complexity routing, model selection, reputation |
| [Monitoring and Intervention](monitoring.md) | Watchdog, supervisor, dashboard, escalations |

## For Contributors

| Guide | Description |
|-------|-------------|
| [Architecture](architecture.md) | Event sourcing, state management, package map, data flow |
| [Contributing](contributing.md) | Adding runtimes, engine components, extending the CLI |

## Demo

Generate an animated GIF of the full VXD workflow with [VHS](https://github.com/charmbracelet/vhs):

```bash
brew install vhs
vhs docs/demo.tape
```

This runs through `vxd init` -> `vxd req` -> `vxd status` -> `vxd agents` -> `vxd events` -> `vxd dashboard`.

## Recommended Reading Order

**New users:** Getting Started -> Tutorial -> Workflows -> Configuration

**Power users:** Agents and Roles -> Monitoring -> Configuration (tuning sections)

**Contributors:** Architecture -> Contributing -> source code
