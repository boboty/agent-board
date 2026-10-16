# Agent Board — Vision and Design Whitepaper

[English](WHITEPAPER.md) | [简体中文](WHITEPAPER.zh-CN.md) | [Back to README](../README.md)

> **Status: design direction, not a released protocol specification.**
>
> Agent Board v0.2.3 is a local-first, AI software-delivery workflow and task ledger. This paper explains the wider collaboration model it may grow toward. It does **not** describe Goals, distributed ledgers, cross-node identity, or automated capability discovery as shipped features.

## 1. Why look beyond a task board?

Stronger models and longer context windows help agents plan, act, and remember. They do not by themselves settle what was authorized, which result was delivered, who independently checked it, and what another participant may safely rely on.

A conversation is a poor unit for durable responsibility. A specific agent or harness may disappear, be replaced, or become unsuitable for the next step. Work still needs a stable definition, evidence, and an acceptance decision.

The initial engineering problem is therefore broader than managing an AI coding queue:

**How can independently operated agents or nodes cooperate without requiring any one participant's memory or judgment to become the shared authority?**

The answer explored here is a small set of collaboration semantics, separate from the model, execution environment, storage engine, and deployment topology.

## 2. Four concepts

**Goal owns the ledger. Task carries the contract. Delivery carries evidence. Governance determines acceptance.**

These are conceptual responsibilities, not a statement that every term is a first-class entity in the current `aboard` binary.

### Goal — the unit of purpose and accountability

A Goal represents an intended outcome and the scope within which work is organized. Its ledger records the tasks, deliveries, evidence, and decisions belonging to that endeavor.

The ledger is **logically associated with the Goal**, not necessarily stored on one server, in one database, or under a global coordinator. A node may organize its own Goal and may simultaneously execute a Task for another Goal.

A Goal is a **future conceptual layer**. In v0.2.3 the implemented scope is a task ledger local to a Git clone, not a networked Goal ledger.

### Task — the unit of delegation

A Task is a contract for an independently delegable unit of work: intended result, constraints, acceptance criteria, and relevant dependencies.

It should not expose or prescribe every internal reasoning step. A receiving node or agent is responsible for executing its assigned Task and returning a delivery within the contract. Whether to promote a substep to a separate Task depends on whether it deserves an independent handoff and acceptance boundary.

### Delivery — an identifiable claim with evidence

A Delivery identifies **what** is offered for acceptance, **which version** of the result is being offered, and **what evidence** supports the claim.

Evidence may be code, tests, documents, measurements, or domain-specific verification material. A report that a task is complete is only a claim until evaluated under the relevant acceptance rules.

The software-delivery reference implementation already records structured `baseline` and `fingerprint` fields to bind verification to a concrete delivery. Other domains will need their own evidence conventions.

### Governance — the authority to accept or reject

Governance answers who may authorize execution, verify results, request correction, accept a delivery, and decide an exception.

Verification and acceptance are distinct. An independent verifier's `PASS` is not the same thing as a Human's explicit acceptance without `PASS`. A governance model may define other authorities in the future, but responsibility and decision provenance must remain explicit.

Governance need not mean a centralized bureaucracy or a particular organization chart. It means that the rules for accepting work are not silently delegated to whoever produced it.

## 3. Nodes, roles, and topology

A **node** is a participant capable of organizing work, executing work, or both. A node may serve as a coordinator for one Goal and a specialized contributor to someone else's Goal. **Orchestrator, Worker, and Verifier are roles in a given collaboration**, not permanent classifications of machines, products, or organizations.

We do not assume that every node must be identical or that the network must be fully decentralized.

- A local node may handle routine execution and user interaction.
- A specialized node may provide expertise, a service, or a trusted verification capability.
- A larger cloud or infrastructure node may outperform smaller nodes even without a narrowly tailored application.

The important design question is not how to eliminate large nodes. It is how to make capabilities, commitments, evidence, reputation, and selection criteria intelligible enough that specialized nodes can participate on their merits. Capability discovery, reputation, compensation, and procurement rules remain research questions, not implemented subsystems.

A collaboration protocol should permit local-only, centrally coordinated, and federated deployments where appropriate. **Decentralization is an option, not a product requirement or ideology.**

## 4. Separate protocol semantics from implementations

The proposed boundary is deliberate:

| Layer | Responsibility |
| --- | --- |
| Collaboration semantics | Goal, Task contract, Delivery evidence, Governance and acceptance |
| Execution environments | Planning, scheduling, reasoning, tools, model selection, agent sessions |
| Engineering or domain standards | Domain-specific quality requirements and verification methods |
| Reference implementation | Local persistence, task-state operations, facts, CLI/MCP/Web access |

The protocol should not require a particular harness, model family, CLI, SQLite schema, or Git workflow. Nor should the reference implementation attempt to absorb every execution feature into its core.

The current division in Agent Board — **code provides capabilities, Skills define workflow rules, and the Board records facts** — is a practical starting point. Protocol evolution should preserve that separation rather than moving agent-specific planning into the ledger.

## 5. Trust, evidence, and limitations

A stored fact is not automatically true. Reliable collaboration depends on identifying evidence, retaining provenance, distinguishing claims from independently obtained checks, and recording decisions clearly.

The existing engineering workflow demonstrates useful mechanisms:

- A Worker delivers a stable, identifiable result.
- A fresh Independent Verifier judges all acceptance criteria and records `PASS`, `RC`, or `BLOCKED`.
- Correction produces a new delivery; evidence reuse requires an explicit applicability assessment.
- A Human acceptance without a verifier `PASS` is recorded separately as a `decision`.

These are **workflow rules**, not a cryptographically enforced trust system. In the current implementation provenance labels are self-reported, the Board does not authenticate roles, and the local SQLite ledger is not tamper-proof. A cross-node system would need explicit approaches to identity, authorization, evidence integrity, and dispute resolution before making stronger trust claims.

## 6. What exists, and what remains open

| Implemented today in the AI engineering reference | Longer-term direction / open design |
| --- | --- |
| Local Tasks and four task states | Goals as ledger-owning collaboration units |
| Structured deliveries, verification facts, decisions and handoffs | Portable contracts and evidence across domains |
| Local ledger shared among worktrees of one Git clone | Cross-node exchange, identity and trust |
| Workflow Skill defining Worker / Verifier separation | Governance schemes for different participants and domains |
| CLI, MCP, and Web interfaces | Capability discovery, federation, and differentiated node participation |

No part of this whitepaper changes the current v0.2.3 API, lifecycle, or compatibility contract. Any new implementation should begin with an observed use case and a clear acceptance model, not abstract infrastructure built in anticipation of a hypothetical network.

## 7. Guiding questions

As practice accumulates, the project should keep testing:

1. Which facts must survive an agent, session, model, or machine switch?
2. What makes a delegated Task sufficiently bounded to be independently accepted?
3. How can evidence be verified without repeating expensive checks unnecessarily?
4. When may a receiving node trust another node's evidence, and under whose authority?
5. How can different-size and different-specialty nodes cooperate without imposing one centralized execution stack?

The intended direction is straightforward: **make work transferable without making responsibility ambiguous**.

For the usable product and its actual guarantees, return to the [README](../README.md), [product definition](../PRODUCT.md), and [workflow specification](../workflow/SKILL.md).
