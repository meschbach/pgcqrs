# Best Practices Documentation Plan

This document outlines the tasks required to document best practices for the system.

## Task List

### 1. Adopt `query2` Package

*   **Description**: Document the process and benefits of migrating existing queries to use the `query2` package.
*   **Sub-tasks**:
    *   Explain the advantages of `query2` over older querying mechanisms (e.g., improved performance, enhanced features).
    *   Provide code examples demonstrating how to refactor common query patterns to use `query2`.
    *   Detail any breaking changes or considerations when migrating to `query2`.
    *   Outline testing strategies to ensure smooth transition and correctness of `query2` implementations.

### 2. Standardize Entity IDs to UUIDs

*   **Description**: Document the best practice of using UUIDs for entity IDs instead of event IDs.
*   **Sub-tasks**:
    *   Explain the rationale behind using UUIDs for entity IDs (e.g., uniqueness, distributed system compatibility, avoiding collisions).
    *   Provide guidance on how to generate and manage UUIDs within the application.
    *   Illustrate how to update existing entities or create new ones with UUIDs as identifiers.
    *   Discuss the impact on data storage, indexing, and querying when using UUIDs.
    *   Address any considerations for backward compatibility or migration strategies for systems currently using event IDs.
