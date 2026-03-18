# Stellar Horizon: Space Agency Management Game
## Problem Statement
Build an engaging, educational space agency management game for a 15-year-old gamer on M2 MacBook Air. The game needs top-quality graphics, storyline, music, physics, and an inspiring tutorial about space exploration.
## Game Concept
**Name**: Stellar Horizon
**Tagline**: "Command the Cosmos. Shape Humanity's Future."
**Genre**: Space Agency Management / Strategy Simulation
**Premise**: You are the newly appointed Director of the Stellar Horizon Space Agency (SHSA). Start with a modest budget and basic facilities. Build your agency from low-Earth orbit missions to interstellar exploration through strategic planning, crew management, and scientific discovery.
## Tech Stack
* **Vite** — fast dev server and build tool
* **HTML5 Canvas** — animated starfield background, launch sequences, orbital visualizations
* **DOM + CSS** — game UI with glassmorphism dark space theme
* **Vanilla JS (ES Modules)** — clean, modular game logic
* **Web Audio API** — procedural ambient music + SFX
* **localStorage** — save/load game state
* No heavy frameworks — lean and fast on M2 hardware
## Visual Design
* Deep space dark theme (#0a0a1a base)
* Neon accent colors: cyan (#00d4ff), purple (#7b2ff7), orange (#ff6b35)
* Glassmorphism panels with backdrop blur
* Animated particle starfield on Canvas
* Smooth CSS transitions and keyframe animations
* Custom pixel-style game font + clean sans-serif for data
## Project Structure
```warp-runnable-command
stellar-horizon/
├── index.html
├── package.json
├── vite.config.js
├── src/
│   ├── main.js              # Entry point
│   ├── game/
│   │   ├── GameState.js      # Central game state manager
│   │   ├── SaveSystem.js     # Save/Load with localStorage
│   │   └── EventBus.js       # Pub/sub event system
│   ├── systems/
│   │   ├── BudgetSystem.js   # Budget management + income
│   │   ├── MissionSystem.js  # Mission catalog, launch, progress
│   │   ├── CrewSystem.js     # Recruitment, training, assignment
│   │   ├── ResearchSystem.js # Tech tree, unlocks
│   │   └── EventSystem.js    # Random events + storyline
│   ├── scenes/
│   │   ├── SceneManager.js   # Scene transitions
│   │   ├── MainMenu.js       # Title screen
│   │   ├── Tutorial.js       # Interactive tutorial
│   │   ├── Dashboard.js      # Agency overview (main hub)
│   │   ├── MissionControl.js # Mission planning + launch
│   │   ├── CrewQuarters.js   # Crew management
│   │   ├── ResearchLab.js    # Tech research
│   │   └── LaunchSequence.js # Animated launch scene
│   ├── canvas/
│   │   ├── Starfield.js      # Animated star background
│   │   ├── Particles.js      # Particle effects
│   │   ├── SolarSystem.js    # Solar system visualization
│   │   └── LaunchAnim.js     # Rocket launch animation
│   ├── audio/
│   │   └── AudioManager.js   # Music + SFX manager
│   ├── data/
│   │   ├── missions.js       # 20+ real space missions
│   │   ├── crew.js           # Crew member templates
│   │   ├── research.js       # Tech tree data
│   │   ├── events.js         # Random event catalog
│   │   ├── spaceFacts.js     # Educational content
│   │   └── tutorial.js       # Tutorial steps
│   ├── ui/
│   │   └── components.js     # Reusable UI component helpers
│   └── utils/
│       ├── constants.js      # Game constants
│       └── helpers.js        # Utility functions
├── public/
│   └── fonts/                # Game fonts
└── styles/
    ├── main.css              # Base styles + theme
    ├── dashboard.css          # Dashboard scene styles
    ├── missions.css           # Mission control styles
    ├── crew.css               # Crew quarters styles
    ├── research.css           # Research lab styles
    ├── tutorial.css           # Tutorial styles
    └── animations.css         # Keyframe animations
```
## Core Game Systems
### 1. Budget System
* Starting budget: $500M
* Income sources: government funding (quarterly), mission rewards, sponsorship deals
* Expenses: crew salaries, facility maintenance, mission costs
* Budget warnings at low thresholds
* Fiscal year cycle with quarterly reports
### 2. Mission System (20+ missions across 5 tiers)
**Tier 1 — Low Earth Orbit**: Satellite Deploy, ISS Resupply, Space Tourism
**Tier 2 — Lunar**: Lunar Orbiter, Moon Landing, Lunar Base Setup
**Tier 3 — Inner Solar System**: Mars Rover, Venus Probe, Mercury Flyby
**Tier 4 — Outer Solar System**: Jupiter Explorer, Saturn Ring Survey, Titan Landing
**Tier 5 — Deep Space**: Interstellar Probe, Exoplanet Survey, Dyson Sphere Prototype
Each mission has: cost, duration, crew requirements, success probability, rewards, and real educational facts.
### 3. Crew System
* Roles: Pilot, Engineer, Scientist, Medical Officer, Mission Specialist
* Stats: Experience, Morale, Health, Skill Level (1-10)
* Training programs to improve skills
* Crew affects mission success probability
* Famous astronaut cameos as recruitable crew
### 4. Research System (Tech Tree)
* Categories: Propulsion, Life Support, Communications, Materials, AI
* Each research unlocks: new missions, better success rates, cost reductions
* Research takes time and funding
### 5. Event System
* Random events: meteor showers, equipment failures, funding cuts, discoveries
* Storyline events at milestones (first orbit, first moon landing, etc.)
* Player choices that affect agency reputation
## Implementation Phases
### Phase 1: Foundation + Core Loop (Current)
* Project setup with Vite
* Animated Canvas starfield background
* Space-themed UI framework (glassmorphism dark theme)
* Scene manager with transitions
* Main menu with animated title
* Dashboard scene (agency overview HUD)
* Budget system with income/expense tracking
* Mission catalog with 20+ real missions
* Mission launch flow with animated launch sequence
* Crew recruitment and assignment basics
* Research tree (simplified)
* Save/load system
* Educational space facts integrated into missions
* Procedural ambient audio + SFX
* Interactive tutorial
* Push to GitHub
### Phase 2: Depth + Polish (Future)
* Full tech tree with dependencies
* Advanced crew training and morale
* Random event system with storyline arcs
* Solar system map visualization
* Mission progress tracking with timeline
* Achievements system
* Statistics and history view
### Phase 3: Advanced Features (Future)
* Multiplayer leaderboards
* Custom mission builder
* Mod support
* Additional campaigns
