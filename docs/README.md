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
| [Model Selection](model-selection.md) | Provider options, execution vs verification tiers, cost comparison |
| [Gemma 4 Guide](gemma-4-guide.md) | Google AI Studio free tier setup, fallback behavior, configuration |
| [Monitoring and Intervention](monitoring.md) | Watchdog, supervisor, dashboard, escalations |

## For Contributors

| Guide | Description |
|-------|-------------|
| [Architecture](architecture.md) | Event sourcing, state management, package map, data flow |
| [Contributing](contributing.md) | Adding runtimes, engine components, extending the CLI |

## Demo

![VXD Demo](https://vhs.charm.sh/vhs-5yT705ybH66DOTmCJKviR8.gif)

`vxd init` -> `vxd req` -> `vxd status` -> `vxd agents` -> `vxd events` -> `vxd dashboard`

<details>
<summary>Re-record the demo locally</summary>

```bash
brew install vhs ffmpeg ttyd
vhs docs/demo.tape
```
</details>

## Recommended Reading Order

**New users:** Getting Started -> Tutorial -> Workflows -> Configuration

**Power users:** Agents and Roles -> Monitoring -> Configuration (tuning sections)

**Contributors:** Architecture -> Contributing -> source code
