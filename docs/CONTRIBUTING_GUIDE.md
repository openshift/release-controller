# Contributing Guide

Thank you for your interest in contributing to Release Controller! This guide will help you understand how to contribute effectively.

## How to Contribute

### 1. Fork and Clone

```bash
# Fork the repository on GitHub, then clone your fork
git clone https://github.com/YOUR_USERNAME/release-controller.git
cd release-controller

# Add upstream remote
git remote add upstream https://github.com/openshift/release-controller.git
```

### 2. Create a Branch

```bash
# Create a feature branch from main
git checkout -b feature/my-feature

# Or a bugfix branch
git checkout -b fix/bug-description
```

### 3. Make Your Changes

- Write clear, readable code
- Follow Go conventions and style
- Add tests for new functionality
- Update documentation as needed

### 4. Test Your Changes

```bash
# Run unit tests
go test ./...

# Build components
make build

# Verify code generation (if changed APIs)
make verify-codegen
```

### 5. Commit Your Changes

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "Add feature: description of changes"
```

**Commit Message Guidelines:**
- Use imperative mood ("Add feature" not "Added feature")
- Keep first line under 72 characters
- Add detailed description if needed
- Reference issues: "Fix #123: description"

### 6. Push and Create Pull Request

```bash
# Push to your fork
git push origin feature/my-feature
```

Then create a Pull Request on GitHub with:
- Clear title and description
- Reference to related issues
- Screenshots/logs if applicable
- Checklist of what was tested

## Branching Model

### Branch Naming

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring
- `test/description` - Test improvements

### Branch Strategy

- **main** - Production-ready code
- **Feature branches** - Created from main, merged back via PR

## Coding Standards

### Go Style

Follow [Effective Go](https://golang.org/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).

**Formatting:**
```bash
# Use gofmt
go fmt ./...

# Or goimports (handles imports)
goimports -w .
```

**Key Guidelines:**
- Use `gofmt` for formatting
- Run `goimports` to organize imports
- Follow naming conventions
- Keep functions focused and small
- Add comments for exported functions/types

### Code Organization

- **Packages**: Group related functionality
- **Files**: Keep files focused
- **Tests**: `*_test.go` files alongside source
- **Test Data**: Use `test/testdata/` directories

### Error Handling

```go
// Good: Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to create release: %w", err)
}

// Good: Use errors.Is and errors.As
if errors.Is(err, os.ErrNotExist) {
    // handle
}
```

### Logging

```go
// Use klog for logging
import "k8s.io/klog"

klog.V(4).Infof("Creating release: %s", releaseTag)
klog.Errorf("Failed to create release: %v", err)
```

### Testing

**Unit Tests:**
```go
func TestFunction(t *testing.T) {
    // Arrange
    input := "test"
    
    // Act
    result := Function(input)
    
    // Assert
    if result != expected {
        t.Errorf("Expected %v, got %v", expected, result)
    }
}
```

## PR Guidelines

### PR Requirements

1. **Description**: Clear description of what and why
2. **Tests**: All tests pass
3. **Code Generation**: Verify codegen if APIs changed
4. **Documentation**: Updated if needed
5. **Size**: Keep PRs focused and reasonably sized

### PR Template

```markdown
## Description
Brief description of changes

## Problem
What problem does this solve?

## Solution
How does this solve the problem?

## Testing
- [ ] Unit tests added/updated
- [ ] Manual testing performed
- [ ] Code generation verified (if APIs changed)

## Related Issues
Fixes #123
Related to #456
```

### Review Process

1. **Automated Checks**: Must pass (tests, codegen, etc.)
2. **Code Review**: At least one approval required
3. **LGTM**: Reviewer says `/lgtm` when satisfied
4. **Approve**: Approver says `/approve` for final approval
5. **Merge**: Automated merge when all conditions met

## Code Review Guidelines

### For Reviewers

**What to Look For:**
- Code correctness and logic
- Test coverage
- Error handling
- Performance considerations
- Security implications
- Documentation completeness

**Review Checklist:**
- [ ] Code follows style guidelines
- [ ] Tests are adequate
- [ ] Error handling is proper
- [ ] Documentation is updated
- [ ] No security issues
- [ ] Performance is acceptable

### For Authors

**Before Requesting Review:**
- Self-review your code
- Run all tests
- Verify code generation
- Update documentation
- Write clear PR description

**During Review:**
- Respond promptly to comments
- Be open to suggestions
- Ask for clarification if needed
- Update code based on feedback

## Testing Requirements

### Unit Tests

- Required for new functionality
- Aim for >80% coverage for new code
- Test edge cases and error conditions

### Integration Tests

- Required for components that modify external state
- Use test data in `test/testdata/`
- Test with real Kubernetes clusters when possible

## Documentation

### Code Documentation

- Document exported functions/types
- Use Go doc comments
- Include examples for complex APIs

### User Documentation

- Update README.md for component changes
- Add examples for new features
- Update this contributing guide if process changes

## Getting Help

### Communication Channels

- **GitHub Issues**: Bug reports and feature requests
- **Pull Requests**: Code contributions and discussions
- **Slack**: #forum-testplatform (for OpenShift team members)

### Resources

- [Go Documentation](https://golang.org/doc/)
- [Kubernetes Contributing Guide](https://github.com/kubernetes/community/blob/master/contributors/guide/README.md)
- [OpenShift CI Documentation](https://docs.ci.openshift.org/)

## Code of Conduct

- Be respectful and inclusive
- Welcome newcomers
- Focus on constructive feedback
- Follow the [Kubernetes Code of Conduct](https://github.com/kubernetes/community/blob/master/code-of-conduct.md)

Thank you for contributing to Release Controller! 🎉

