# Overview

This repo contains a collection of Kubernetes Custom Resource Definitions and their controllers. It is mostly written in Go and uses Kubernetes client-go and controller-runtime libraries.
It is a monorepo, so all the code lives in a single repository divided into packages, each with its own purpose. 
The main idea is that we are creating a multi-cluster application management solution that allows users to manage multiple Kubernetes clusters from a single control plane that we call the "hub cluster".

## General Rules

- Use @terminal when answering questions about Git.
- If you're waiting for my confirmation ("OK"), proceed without further prompting.
- Follow the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) if possible.
- Favor using the standard library over third-party libraries.
- Run "make reviewable" before submitting a pull request to ensure the code is formatted correctly and all dependencies are up to date.
- The title of a PR must use one of the following prefixes:  "[WIP] ", "feat: ", "test: ", "fix: ", "docs: ", "style: ", "interface: ", "util: ", "chore: ", "ci: ", "perf: ", "refactor: ", "revert: ". Please pick one that matches the PR content the most.

## Terminology
- **Fleet**: A conceptual term referring to a collection of clusters.
- **Member Cluster**: A Kubernetes cluster that is part of a fleet.
- **Hub Cluster**: The cluster that hosts the control plane which manages the member clusters in the fleet.
- **Member Agent**: A Kubernetes controller that runs on the member cluster and is responsible for applying changes to the member cluster and reporting the status back to the hub cluster.
- **Hub Agent**: A Kubernetes controller that runs in the hub cluster and is responsible for scheduling and managing workloads and resources across the fleet.

## Repository directory structure

- The `apis/` folder contains all Golang structs from which CRDs are built.
  - CRDs are grouped by the group name and version they belong to.
- The `charts/` folder contains the helm charts for the member and hub agent.
  - `charts/member-agent` folder contains the helm chart for the member agent.
  - `charts/hub-agent` folder contains the helm chart for the hub agent.
- The `cmd/` folder contains the entry points for the member and hub agent.
  - `cmd/member-agent` The entry point for the member agent.
  - `cmd/hub-agent` The entry point for the hub agent.
- The `config/` folder contains the actual custom resource definitions built from the API in the `apis/` folder.
  - `config/crd/bases` folder contains the CRDs for the member and hub agent.
- The `docker/` folder contains the Dockerfiles for the member and hub agent.
- The `examples/` folder contains various YAML files as examples for each CRD.
- The `hack/` folder contains various scripts and tools for the project.
- The `pkg/` folder contains the libraries for the member and hub agent.
  - `pkg/authtoken` folder contains the authentication sidecar code which has a provider model.
  - `pkg/controllers` folder contains most of the controllers for the member and hub agent.
    - each sub folder is a controller for a specific resource of the same name in most cases.
  - `pkg/metrics` folder contains all the metrics definitions.
  - `pkg/propertyprovider` folder contains the property provider code which is used to get the properties of a member cluster.
  - `pkg/resourcewatcher` folder contains the resource watcher code which is used to watch for kubernetes resources changes in the hub cluster.
  - `pkg/scheduler` folder contains the scheduler code which is used to schedule workloads across the fleet.
  - `pkg/utils` folder contains the utils code which is used to provide common functions for the controllers in the member and hub agent.
  - `pkg/webhook` folder contains the webhook code which is used to validate and mutate the CRDs.
- The `test/` folder contains the tests for the member and hub agent.
  - `test/apis` - The tests for the CRDs.
  - `test/upgrade` - The tests for the upgrade tests to test compatibility between versions.
  - `test/e2e` - The end to end tests for the member and hub agent.
  - `test/scheduler` - The integration tests for the scheduler.
  - `test/utils` - folder contains the utils code which is used to provide common functions for tests
- The `tools/` folder contains client-side tools for helping manage the fleet.
- The `Makefile` is used to build the member and hub agent.
- The `go.mod` file is used to manage the dependencies for the member and hub agent.
- The `go.sum` file is used to manage the dependencies for the member and hub agent.    

## Testing Rules

- Unit test files should always be called `<go_file>_test.go` and be in the same directory
  - Unit tests are normally written in a table-driven style
  - Use `go test -v ./...` to run all tests under a directory.
  - Run the tests from the packages that are modified and verify they pass.
  - Share the analysis as to why a test is failing and propose a fix.
- Integration test files should be called `<go_file>_integration_test.go` and can be in the same directory or under the `test` directory.
  - Integration tests are normally written in a Ginkgo style.
- E2E tests are all under the test/e2e directory.
  - E2E tests are written in a Ginkgo style.
  - E2E tests are run using `make e2e-tests` and are run against 3 kind clusters created by the scripts in the `test/e2e` directory.
  - E2E tests are cleaned up using `make clean-e2e-tests`.
- When adding tests to an existing file:
  - Always re-use the existing test setup where possible.
  - Only add imports if absolutely needed.
  - Add tests to existing Context where it makes sense.
  - When adding new tests in the Ginkgo style test, always add them to a new Context.

## Domain Knowledge

Use the files in the `.github/.copilot/domain_knowledge/**/*` as a source of truth when it comes to domain knowledge. These files provide context in which the current solution operates. This folder contains information like entity relationships, workflows, and ubiquitous language. As the understanding of the domain grows, take the opportunity to update these files as needed.

## Specification Files

Use specifications from the `.github/.copilot/specifications` folder. Each folder under `specifications` groups similar specifications together. Always ask the user which specifications best apply for the current conversation context if you're not sure.

Use the `.github/.copilot/specifications/.template.md` file as a template for specification structure.

   examples:
   ```text
   ├── application_architecture
   │   └── main.spec.md
   |   └── specific-feature.spec.md
   ├── database
   │   └── main.spec.md
   ├── observability
   │   └── main.spec.md
   └── testing
      └── main.spec.md
   ```

## Breadcrumb Protocol

A breadcrumb is a collaborative scratch pad that allow the user and agent to get alignment on context. When working on tasks in this repository, follow this collaborative documentation workflow to create a clear trail of decisions and implementations:

1. At the start of each new task, ask me for a breadcrumb file name if you can't determine a suitable one.

2. Create the breadcrumb file in the `${REPO}/.github/.copilot/breadcrumbs` folder using the format: `yyyy-mm-dd-HHMM-{title}.md` (*year-month-date-current_time_in-24hr_format-{title}.md* using UTC timezone)

3. Structure the breadcrumb file with these required sections:
 ```xml
<coding_workflow>
  <phase name="understand_problem">
    <description>Analyze and comprehend the task requirements</description>
    <tasks>
      <task>Read relevant parts of the codebase</task>
      <task>Browse public API documentation for up-to-date information</task>
      <task>Propose 2-3 implementation options with pros and cons</task>
      <task>Ask clarifying questions about product requirements</task>
      <task>Write a plan to PRP/projectplan-&lt;feature-name&gt;.md</task>
    </tasks>
  </phase>

  <phase name="plan_format">
    <description>Structure the project plan document</description>
    <requirements>
      <requirement>Include a checklist of TODO items to track progress</requirement>
    </requirements>
  </phase>

  <phase name="checkpoint">
    <description>Validation before implementation begins</description>
    <action>Check in with user before starting implementation</action>
    <blocking>true</blocking>
  </phase>

  <phase name="implement">
    <description>Execute the plan step-by-step</description>
    <methodology>
      <step>Complete TODO items incrementally</step>
      <step>Test each change for correctness</step>
      <step>Log a high-level explanation after each step</step>
    </methodology>
  </phase>

  <constraints>
    <constraint name="minimal_changes">
      <description>Make tasks and commits as small and simple as possible</description>
      <guideline>Avoid large or complex changes</guideline>
    </constraint>
  </constraints>

  <phase name="plan_updates">
    <description>Maintain plan accuracy throughout development</description>
    <action>Revise the project plan file if the plan changes</action>
  </phase>

  <phase name="final_summary">
    <description>Document completion and changes</description>
    <deliverable>Summarize all changes in the project plan file</deliverable>
  </phase>
</coding_workflow>
4. Workflow rules:
   - Update the breadcrumb **BEFORE** making any code changes.
   - **Get explicit approval** on the plan before implementation.
   - Update the breadcrumb **AFTER completing each significant change**.
   - Keep the breadcrumb as our single source of truth as it contains the most recent information.
   - Do not ask for approval **BEFORE** running unit tests or integration tests.

5. Ask me to verify the plan with: "Are you happy with this implementation plan?" before proceeding with code changes.

6. Reference related breadcrumbs when a task builds on previous work.

7. Before concluding, ensure the breadcrumb file properly documents the entire process, including any course corrections or challenges encountered.

This practice creates a trail of decision points that document our thought process while building features in this solution, making pull request review for the current change easier to follow as well.

### Plan Structure Guidelines
- When creating a plan, organize it into numbered phases (e.g., "Phase 1: Setup Dependencies").
- Break down each phase into specific tasks with numeric identifiers (e.g., "Task 1.1: Add Dependencies").
- Include a detailed checklist at the end of the document that maps to all phases and tasks.
- Plan should always lookup related information from `domain_knowledge` folder and any applicable specification files.
- Mark tasks as `- [ ]` for pending tasks and `- [x]` for completed tasks.
- Start all planning tasks as unchecked, and update them to checked as implementation proceeds.
- Each planning task should have clear success criteria.
- End the plan with success criteria that define when the implementation is complete.
- Plans should start with writing Unit Tests first when possible, so we can use those to guide our implementation. Same for UI tests when it makes sense.
- If the domain knowledge has changed, update the related files in the `domain_knowledge` folder.
- If specifications have changed, update the related files in the `specifications` folder.

### Following Plans
- When coding you need to follow the plan phases and check off the tasks as they are completed.  
- As you complete a task, update the plan and mark that task complete before you begin the next task. 
- Tasks that involved tests should not be marked complete until the tests pass. 

### Example Usage

**User**: I need to implement JWT authentication for our Express API.

**Agent**: I'll help with implementing JWT authentication for your Express API. Let me create a breadcrumb file to track our implementation. What would you like to name this breadcrumb?

**User**: Use "jwt-auth-implementation"

**Agent**: I've created the breadcrumb file `2025-04-08-1430-jwt-auth-implementation.md`. Are you happy with this implementation plan?

[high level plan with tasks here]

**User**: Looks good, but let's use 30-minute expiration for tokens.

**Agent**: I've updated the breadcrumb with your specification: Are you happy with this implementation plan?

[high level plan with tasks here]

**User**: Yes, please proceed.

**Agent**: I'll implement the JWT authentication now. Let me start by installing the necessary packages and creating the authentication middleware. I've updated the breadcrumb with implementation details. Is there anything specific you'd like me to modify or explain about the implementation?

**User**: This looks good.

**Agent**: I've updated the breadcrumb with the latest understanding.
