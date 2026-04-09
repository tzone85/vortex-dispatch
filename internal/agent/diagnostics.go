package agent

// DiagnosticPlaybook contains structured debugging methodologies
// injected into agent system prompts when working on existing codebases.
// These are not suggestions — they are step-by-step workflows the agent
// MUST follow before writing any code.

// CodebaseArchaeology is the methodology for understanding an unfamiliar codebase.
// Injected into TechLead prompts when decomposing work on existing repos.
const CodebaseArchaeology = `
## Codebase Archaeology Methodology (EXECUTE BEFORE PLANNING)

You are entering an unfamiliar codebase. Before creating ANY stories, execute this
diagnostic sequence in order. Do not skip steps.

### Step 1: Orientation (2 minutes)
Run these commands and read the output carefully:
  ls -la                           # What's at the root?
  cat README.md 2>/dev/null        # Official docs
  cat CLAUDE.md 2>/dev/null        # AI agent instructions
  cat Makefile 2>/dev/null         # Build system
  cat docker-compose.yml 2>/dev/null  # Infrastructure
  cat .env.example 2>/dev/null     # Required config

### Step 2: Architecture Map (3 minutes)
  find . -type f -name "*.go" -o -name "*.py" -o -name "*.ts" -o -name "*.js" -o -name "*.rs" -o -name "*.java" | head -50
  # Identify: entry points (main.go, index.ts, app.py), config files, test files
  wc -l $(find . -type f -name "*.go" 2>/dev/null) | sort -n | tail -20  # Largest files = architectural hotspots
  cat go.mod 2>/dev/null || cat package.json 2>/dev/null || cat requirements.txt 2>/dev/null  # Dependencies

### Step 3: History & Intent (2 minutes)
  git log --oneline -20            # Recent trajectory
  git log --oneline --all --graph -10  # Branch structure
  git shortlog -sn | head -10     # Who wrote this?
  git log --diff-filter=A --name-only --pretty="" | head -20  # File creation order = build order

### Step 4: Health Check (2 minutes)
  # Try to build — does it even compile?
  go build ./... 2>&1 || npm run build 2>&1 || python -m py_compile *.py 2>&1
  # Try to test — is there a test suite?
  go test ./... 2>&1 | tail -20 || npm test 2>&1 | tail -20 || pytest 2>&1 | tail -20
  # Check for running services
  docker ps 2>/dev/null
  lsof -i -P -n 2>/dev/null | grep LISTEN | head -10

### Step 5: Dependency Graph (2 minutes)
  # For Go:
  go mod graph 2>/dev/null | head -20
  grep -r "import" --include="*.go" -l | head -20
  # For Node:
  cat package.json 2>/dev/null | grep -A50 '"dependencies"'
  # For Python:
  cat requirements.txt 2>/dev/null || cat pyproject.toml 2>/dev/null

### Step 6: Smell Detection
Look for these red flags and document them:
  - Files over 500 lines (complexity hotspots)
  - No test files (untested code = risky to change)
  - TODO/FIXME/HACK comments (known debt)
  - Commented-out code (indecision or fear of deletion)
  - Deep nesting (>4 levels of if/for)
  - God objects (files that import everything)
  - Missing error handling (silent failures)
  - Hardcoded values (config that should be external)

grep -rn "TODO\|FIXME\|HACK\|XXX" --include="*.go" --include="*.py" --include="*.ts" --include="*.js" | head -20

OUTPUT: Before creating stories, write a brief architecture summary:
  1. Entry point(s)
  2. Key abstractions/patterns
  3. Test coverage status (has tests? do they pass?)
  4. Build status (compiles? runs?)
  5. Known issues found during archaeology
  6. Risk areas (untested, complex, or fragile code)
`

// BugHuntingMethodology is the structured approach to diagnosing and fixing bugs.
// Injected into Senior/Intermediate prompts when the story involves debugging.
const BugHuntingMethodology = `
## Bug Hunting Methodology (FOLLOW THIS SEQUENCE)

### Phase 1: Reproduce (MANDATORY — do not skip)
Before attempting ANY fix, you MUST reproduce the bug:
  1. Read the bug report/error message word by word
  2. Find or write a test that triggers the exact failure
  3. Run it. See it fail. Screenshot/log the failure.
  4. If you can't reproduce it, investigate environment differences

### Phase 2: Isolate
Narrow down where the bug lives:
  1. Read the stack trace bottom-to-top (the cause is usually near the bottom)
  2. Add targeted log statements or use a debugger
  3. Binary search: comment out half the code path — does the bug persist?
  4. Check recent changes: git log --oneline -10 -- <affected_file>
  5. Check if the bug exists on main/master: git stash && test && git stash pop

### Phase 3: Understand Root Cause
Before writing the fix, answer these questions:
  1. What is the EXPECTED behavior?
  2. What is the ACTUAL behavior?
  3. What specific line/function causes the divergence?
  4. WHY does that line behave incorrectly? (Not "what" — "why")
  5. When was this introduced? Was it always broken or did it regress?

### Phase 4: Fix
  1. Write the minimal fix that addresses the root cause
  2. Do NOT refactor surrounding code in the same commit
  3. Do NOT fix other bugs you noticed — file them separately
  4. Verify your failing test now passes
  5. Run the FULL test suite — check for regressions

### Phase 5: Verify
  1. Run all tests: go test ./... -race (or equivalent)
  2. Try to break your fix: what edge cases exist?
  3. Add a test for each edge case
  4. Check that the original error message/condition cannot recur

### Common Bug Patterns to Check
  - Nil/null pointer: is the object initialized? checked before use?
  - Race condition: concurrent access without synchronization?
  - Off-by-one: loop bounds, slice indices, string lengths?
  - Type coercion: implicit conversion losing precision? int overflow?
  - Environment: different behavior local vs CI vs production?
  - State mutation: shared mutable state modified unexpectedly?
  - Error swallowing: catch/recover that hides the real error?
  - Zero values: Go structs with zero-value fields that silently disable logic?
  - Resource leaks: unclosed file handles, DB connections, HTTP bodies?
  - Encoding: UTF-8 vs ASCII, URL encoding, JSON escaping?
`

// InfrastructureDebugging is the methodology for diagnosing deployment,
// Docker, CI/CD, and infrastructure issues.
const InfrastructureDebugging = `
## Infrastructure Debugging Toolkit

### Docker Issues
  docker ps -a                     # Running + stopped containers
  docker logs <container> --tail 50  # Recent logs
  docker inspect <container>       # Full config (env, mounts, network)
  docker exec -it <container> sh   # Shell into container
  docker-compose config            # Validate compose file
  docker system df                 # Disk usage (full disk = crashes)
  docker network ls && docker network inspect <network>  # Network issues

### Database Issues
  # PostgreSQL
  psql -c "SELECT version();"
  psql -c "SELECT pg_is_in_recovery();"  # Is it a replica?
  psql -c "SELECT * FROM pg_stat_activity WHERE state != 'idle';"  # Active queries
  psql -c "SELECT schemaname, tablename FROM pg_tables WHERE schemaname = 'public';"

  # SQLite
  sqlite3 <db> ".tables"
  sqlite3 <db> "PRAGMA integrity_check;"
  sqlite3 <db> "PRAGMA journal_mode;"  # Should be WAL for concurrent access

  # MySQL
  mysql -e "SHOW PROCESSLIST;"
  mysql -e "SHOW TABLE STATUS;"

### CI/CD Pipeline Issues
  # Check recent workflow runs
  gh run list --limit 5
  gh run view <run-id> --log-failed  # Only failed step logs
  # Common causes: expired secrets, dependency version drift, disk space, timeout

### Network Issues
  curl -v https://api.example.com/health  # Verbose HTTP (shows TLS, headers, timing)
  dig example.com                  # DNS resolution
  nslookup example.com
  lsof -i :8080                    # What's using a port?
  netstat -tlnp 2>/dev/null || ss -tlnp  # All listening ports

### Environment Issues
  env | sort                       # All environment variables
  echo $PATH | tr ':' '\n'       # PATH analysis
  which <binary>                   # Which version are we running?
  <binary> --version              # Version check
  cat /etc/os-release 2>/dev/null  # OS info
  df -h                           # Disk space
  free -m 2>/dev/null || vm_stat  # Memory (Linux vs macOS)

### Log Analysis
  # Find errors in logs
  grep -i "error\|fatal\|panic\|exception\|fail" <logfile> | tail -20
  # Find patterns
  grep -c "error" <logfile>        # Error frequency
  tail -f <logfile>                # Live tail
  journalctl -u <service> --since "1 hour ago"  # systemd services

### Common Infrastructure Failures
  - Port already in use: another instance running? lsof -i :<port>
  - Permission denied: file ownership? chmod/chown needed? SELinux?
  - Disk full: df -h; docker system prune if Docker
  - DNS resolution: /etc/hosts override? VPN interfering?
  - TLS/SSL: certificate expired? self-signed vs CA? wrong domain?
  - Memory: OOM killer? check dmesg | grep -i oom
  - Timeout: slow database query? network latency? connection pool exhausted?
`

// LegacyCodeSurvival is the methodology for working with poorly structured codebases
// without making them worse.
const LegacyCodeSurvival = `
## Legacy Code Survival Guide

### Golden Rules
1. NEVER rewrite from scratch — it always takes longer than you think
2. Make the change easy, then make the easy change (Kent Beck)
3. Leave the code better than you found it — but only the code you touch
4. Characterization tests first: document what the code DOES before changing what it SHOULD do
5. Small, safe steps: commit after every working state

### Working with Code You Don't Understand
1. Start from the entry point (main, index, app) and trace the call path to the bug
2. Use grep liberally: "where is this function called?" "where is this variable set?"
3. Read the tests (if any) — they document intended behavior
4. Read git blame on confusing code — the commit message may explain WHY
5. Look for patterns: if 3 files follow a pattern and 1 doesn't, the 1 is probably wrong

### Safe Refactoring Steps
1. Extract: pull a block of code into a named function (easiest, lowest risk)
2. Rename: give things accurate names (surprisingly high impact)
3. Remove dead code: delete unused functions/variables (use grep to verify)
4. Add types: replace any/interface{} with concrete types where possible
5. Add error handling: replace silent failures with explicit errors

### What NOT to Do
- Don't restructure directories in a bug fix PR
- Don't change indentation/formatting in files you didn't modify functionally
- Don't add abstractions "for the future"
- Don't fix every problem you see — stay focused on your story
- Don't remove backwards compatibility without explicit approval

### Dealing with No Tests
If the codebase has no tests:
1. Write characterization tests for the code you need to change
   (tests that pass with current behavior, even if that behavior is "wrong")
2. Make your change
3. Verify characterization tests still pass (no unintended side effects)
4. Add targeted tests for the NEW behavior
5. Commit the characterization tests separately from the fix
`
