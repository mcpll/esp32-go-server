## PR Type

Indicate what kind of change does this PR introduce?

- [ ] Bugfix
- [ ] Feature
- [ ] CI/Build related changes
- [ ] Refactoring (no functional changes, no api changes)
- [ ] Code style update (formatting, local variables)
- [ ] Documentation content changes
- [ ] Tests
- [ ] Other

## Description

Please remove the sections that are not relevant to this PR type.

- [ ] Describe the bug that was fixed.
- [ ] Describe the fix that was implemented.
- [ ] If applicable, add a test that demonstrates the bug was fixed.

### :sparkles: Is it a feature?

- [ ] Describe the feature that was implemented.
- [ ] If applicable, add a test that demonstrates the feature works.

### :hammer: For all other changes

- [ ] Describe the change that was made.

### For all changes

- [ ] Update README.md if applicable.
- [ ] Update the REST API doc if applicable.
- [ ] Update the frontend WEBSOCKET doc if applicable.
- [ ] Update the robot WEBSOCKET doc if applicable.

## How to test? (if applicable)

If this PR introduces a code change, how to test it?

### Example for a backend change:

- Run `curl -X GET -H "Content-Type: application/json" http://localhost:8080/api/bad` and verify you get a 404
- `curl -X GET -H "Content-Type: application/json" http://localhost:8080/api/good` should reply

```json
{
  "good": "json"
}
```

### Example for a frontend change:

- Open `http://localhost:3000/fux/ciao`
- Click on the unicorn button
- `http://backend/api/unicorn/42` should get called

