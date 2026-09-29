# Security policy

## Report a vulnerability

Use GitHub private vulnerability reporting on this repository. Do not open a public issue for a security problem.

## What the operator holds

- A credential for the Ziti Edge Management API. It gives full control of the network. Store it in a Secret that only the operator can read.
- Enrollment JWTs and identity files in Secrets that the operator creates. The operator never writes them to status, events, logs, or metrics.

## Scope of a report

In scope: leaking secrets, acting on entities the operator does not own, and bypassing `roleScope` or `allowedNamespaces`.
