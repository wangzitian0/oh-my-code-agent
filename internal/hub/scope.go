package hub

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// Scope declares where one running hub lives, on the axis
// internal/domain.RuntimeScope defines.
//
// The hub is user-scoped: DefaultSocketPath and DefaultConfigPath both
// derive from the real home and never from a worktree, so one daemon serves
// every worktree and every window one OS user has open. Profiles are
// multiplexed inside that single process (Config.ResolveServer picks one per
// request from workspace_roots), not by running more copies.
//
// Declaring it is what distinguishes this from the thing init.md's Non-goals
// rule out. OMCA still does not decide which host works on what. What it now
// admits to doing is owning the scope at which shared things live -- which a
// per-worktree isolation model does not get to opt out of, because the
// alternative is not "no shared scope", it is paying for every shared thing
// once per checkout.
func Scope() domain.RuntimeScope { return domain.RuntimeScopeUser }
