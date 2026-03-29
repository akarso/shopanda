# AGENTS.md

## Purpose

This document defines **global collaboration rules** for both human contributors and LLM agents working on this project.

These rules apply across the entire system (core, plugins, API, optional frontend).

---

## Core Principles

* Small, reviewable changes over large rewrites
* Explicit architecture over hidden magic
* Composition over inheritance
* Deterministic behavior over implicit side effects
* Core remains minimal and stable
* Complexity must be **opt-in (via plugins/modules)**

---

## Development Workflow

* Work is delivered via **small, focused PRs**
* One PR = one responsibility (strict)
* Each PR must be:

  * runnable
  * testable (when applicable)
  * reviewable in ~10–20 minutes

### PR Guidelines

* Avoid “big bang” implementations
* Prefer vertical slices (HTTP → application → domain)
* Do not introduce unused abstractions
* Do not pre-build for hypothetical use cases

---

## Architecture Rules (Global)

### Style

* Hexagonal architecture (ports & adapters)
* Domain-first design
* No framework-driven structure

---

### Dependency Direction

```text
interfaces → application → domain
                ↓
         infrastructure
```

### Rules

* Domain layer:

  * no database access
  * no HTTP logic
  * no infrastructure dependencies

* Application layer:

  * orchestrates use cases
  * coordinates domain + infrastructure

* Infrastructure:

  * implements interfaces (DB, cache, plugins, etc.)

---

## Extensibility Model (CRITICAL)

The system is extended via **explicit mechanisms only**:

### 1. Events

* async or sync reactions
* no ordering guarantees

### 2. Pipelines

* deterministic transformations (e.g. pricing)

### 3. Workflows

* ordered, stateful flows (e.g. checkout)

### 4. Composition Pipelines

* API response building (PDP, PLP)

---

### Rules

* Do NOT override core logic directly
* Do NOT modify core behavior implicitly
* All extensions must go through:

  * events
  * hooks
  * pipelines
  * workflows

---

## Plugin Philosophy

* Plugins provide **optional complexity**
* Core defines contracts; plugins provide implementations

### Examples:

* search (postgres / meilisearch)
* cache (none / redis)
* API (REST / GraphQL)
* frontend (none / custom)

---

### Plugin Rules

* Plugins must be isolated
* Plugins must not mutate core schema
* Plugins must not introduce hidden side effects
* Plugins must be explicit in registration and behavior

---

## Testing Rules

* Tests are encouraged, but **not required in early PRs**
* Focus on:

  * correctness
  * determinism
  * reproducibility

### Later stages:

* Domain → unit tests
* Application → flow tests
* Integration → API + DB

---

## Code Quality

* Prefer simple, readable code over abstraction
* Avoid deep nesting and long functions
* One responsibility per function/module
* No “utils dumping ground”

---

## Go-Specific Guidelines

* Prefer explicit wiring over DI frameworks
* Use interfaces at boundaries only
* Avoid unnecessary abstractions
* Keep packages small and focused

---

## Communication Rules for LLMs

* If uncertain → ask
* Do not hallucinate APIs or structures
* Follow existing project structure strictly
* Do not introduce scope creep
* Do not “optimize” prematurely

---

## Anti-Patterns (Strictly Forbidden)

* ❌ Core overrides (Magento-style)
* ❌ Hidden magic / reflection-based behavior
* ❌ Global state mutation
* ❌ Large, multi-concept PRs
* ❌ Premature generalization

---

## Repository-Specific Rules

Additional documents:

* `BACKEND.md` — backend-specific modules & sequencing
* `FRONTEND.md` — frontend strategy (optional/headless)
* `PLUGINS.md` — plugin authoring guide

## Documentation

* create /docs/prs/PR-00n.md with summary of changes after each implementation phase

---

## Guiding Principle

> Build a system where complex things are possible,
> without making simple things hard.

Clarity beats cleverness. Small steps beat big rewrites.
