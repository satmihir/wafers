package cli

const skillMarkdown = `# wafers

Use wafers when you need a cheap, branch-backed repo view for a coding task.
wafers is for filesystem fanout, not security isolation.

## When To Use

Use wafers when:

- you need to work in a repo without touching the base checkout
- multiple agents may work from the same large repo at the same time
- you want changes committed to a normal local Git branch

Do not treat wafers as a sandbox. If the task needs security isolation, use a
container or sandbox outside wafers.

## Workflow

1. Start from the base repo, not an existing wafer.
2. Check the host once:

   ` + "```sh" + `
   wafers doctor
   ` + "```" + `

3. Create one wafer per task with a unique name, mountpoint, and branch:

   ` + "```sh" + `
   wafers add <name> --from <base-repo> --at <mountpoint> --branch <branch>
   ` + "```" + `

4. Work only inside the wafer mountpoint.
5. Do not run Git commands inside the wafer. wafers hides .git there on purpose.
6. Commit through wafers:

   ` + "```sh" + `
   wafers git-commit <name> -m "<message>"
   ` + "```" + `

7. Check wafer status and commit pointers:

   ` + "```sh" + `
   wafers ls
   ` + "```" + `

8. Inspect or push the branch from the base repo:

   ` + "```sh" + `
   git -C <base-repo> log --oneline --decorate --graph --all
   git -C <base-repo> push origin <branch>
   ` + "```" + `

9. Clean up the wafer when finished:

   ` + "```sh" + `
   cd /tmp
   wafers rm <name> --force
   ` + "```" + `

## Rules

- Never edit the base repo worktree for wafer task changes.
- Before creating a wafer, keep the base repo clean except for ignored files.
- Never use the base repo index as part of the task.
- Use unique branch names; wafers add refuses an existing branch.
- Mount wafers outside other Git repos so Git cannot discover a parent .git.
- If ` + "`wafers rm`" + ` says the device is busy, leave the mountpoint with ` + "`cd /tmp`" + ` and retry.
- ` + "`wafers rm`" + ` removes wafer state, but keeps the local branch and commits.

## Typical Session

` + "```sh" + `
wafers add parser-fix --from /repo --at /tmp/parser-fix --branch agents/parser-fix
cd /tmp/parser-fix
# edit files
wafers git-commit parser-fix -m "fix parser"
wafers ls
git -C /repo show --stat agents/parser-fix
git -C /repo push origin agents/parser-fix
cd /tmp
wafers rm parser-fix --force
` + "```" + `
`
