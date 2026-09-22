# Contributing to RouteX

Thanks for your interest in RouteX.

## Development principles

- Keep the gateway data plane independent from asynchronous analytics.
- Prefer explicit domain boundaries over shared global state.
- Preserve provider-native behavior where RouteX does not intentionally define an abstraction.
- Treat authentication, credentials, quotas, routing, and audit data as security-sensitive.
- Add tests for behavior changes and bug fixes.

## Licensing

Unless a file or directory explicitly states otherwise, contributions to the
open-source portion of RouteX are accepted under the MIT License.

Do not submit code to the `enterprise/` directory unless the maintainer has
explicitly requested it and the applicable contribution terms have been agreed.

By submitting a contribution, you represent that you have the right to submit
it under the applicable project license.

## Pull requests

Keep pull requests focused. Explain the problem being solved, the behavior
change, and any compatibility or migration impact. Larger architectural changes
should be documented before substantial implementation begins.
