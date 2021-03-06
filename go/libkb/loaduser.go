// Copyright 2015 Keybase, Inc. All rights reserved. Use of
// this source code is governed by the included BSD license.

package libkb

import (
	"fmt"
	"runtime/debug"

	"golang.org/x/net/context"

	keybase1 "github.com/keybase/client/go/protocol/keybase1"
	jsonw "github.com/keybase/go-jsonw"
)

type LoadUserArg struct {
	Contextified
	uid                      keybase1.UID
	name                     string // Can also be an assertion like foo@twitter
	publicKeyOptional        bool
	noCacheResult            bool // currently ignore
	self                     bool
	forceReload              bool
	forcePoll                bool // for cached user load, force a repoll
	staleOK                  bool // if stale cached versions are OK (for immutable fields)
	cachedOnly               bool // only return cached data (StaleOK should be true as well)
	loginContext             LoginContext
	abortIfSigchainUnchanged bool
	resolveBody              *jsonw.Wrapper // some load paths plumb this through

	// NOTE: We used to have these feature flags, but we got rid of them, to
	// avoid problems where a yes-features load doesn't accidentally get served
	// the result of an earlier no-features load from cache. We shouldn't add
	// any more flags like this unless we also add machinery to avoid that
	// mistake.
	// AllKeys      bool
	// AllSubchains bool

	// We might have already loaded these if we're falling back from a
	// failed LoadUserPlusKeys load
	merkleLeaf *MerkleUserLeaf
	sigHints   *SigHints

	// NetContext is the context to build on top of.  We'll make a new
	// Debug Tag for this LoadUser Operation.
	netContext context.Context
}

func (arg LoadUserArg) String() string {
	return fmt.Sprintf("{UID:%s Name:%q PublicKeyOptional:%v NoCacheResult:%v Self:%v ForceReload:%v ForcePoll:%v StaleOK:%v AbortIfSigchainUnchanged:%v CachedOnly:%v}",
		arg.uid, arg.name, arg.publicKeyOptional, arg.noCacheResult, arg.self, arg.forceReload,
		arg.forcePoll, arg.staleOK, arg.abortIfSigchainUnchanged, arg.cachedOnly)
}

func NewLoadUserArg(g *GlobalContext) LoadUserArg {
	return LoadUserArg{Contextified: NewContextified(g)}
}

func NewLoadUserArgWithContext(ctx context.Context, g *GlobalContext) LoadUserArg {
	return LoadUserArg{
		Contextified: NewContextified(g),
		netContext:   ctx,
	}
}

func NewLoadUserSelfArg(g *GlobalContext) LoadUserArg {
	ret := NewLoadUserArg(g)
	ret.self = true
	return ret
}

func NewLoadUserSelfAndUIDArg(g *GlobalContext) LoadUserArg {
	ret := NewLoadUserArg(g)
	ret.self = true
	ret.uid = g.GetMyUID()
	return ret
}

func NewLoadUserForceArg(g *GlobalContext) LoadUserArg {
	arg := NewLoadUserPubOptionalArg(g)
	arg.forceReload = true
	return arg
}

func NewLoadUserByNameArg(g *GlobalContext, name string) LoadUserArg {
	arg := NewLoadUserArg(g)
	arg.name = name
	return arg
}

func NewLoadUserByUIDArg(ctx context.Context, g *GlobalContext, uid keybase1.UID) LoadUserArg {
	arg := NewLoadUserArg(g)
	arg.uid = uid
	arg.netContext = ctx
	return arg
}

func NewLoadUserByUIDForceArg(g *GlobalContext, uid keybase1.UID) LoadUserArg {
	arg := NewLoadUserArg(g)
	arg.uid = uid
	arg.forceReload = true
	return arg
}

func NewLoadUserPubOptionalArg(g *GlobalContext) LoadUserArg {
	arg := NewLoadUserArg(g)
	arg.publicKeyOptional = true
	return arg
}

func (arg LoadUserArg) WithSelf(self bool) LoadUserArg {
	arg.self = self
	return arg
}

func (arg LoadUserArg) WithCachedOnly() LoadUserArg {
	arg.cachedOnly = true
	return arg
}

func (arg LoadUserArg) WithResolveBody(r *jsonw.Wrapper) LoadUserArg {
	arg.resolveBody = r
	return arg
}

func (arg LoadUserArg) WithName(n string) LoadUserArg {
	arg.name = n
	return arg
}

func (arg LoadUserArg) WithNetContext(ctx context.Context) LoadUserArg {
	arg.netContext = ctx
	return arg
}

func (arg LoadUserArg) WithUID(uid keybase1.UID) LoadUserArg {
	arg.uid = uid
	return arg
}

func (arg LoadUserArg) WithPublicKeyOptional() LoadUserArg {
	arg.publicKeyOptional = true
	return arg
}

func (arg LoadUserArg) WithForcePoll(fp bool) LoadUserArg {
	arg.forcePoll = fp
	return arg
}

func (arg LoadUserArg) WithStaleOK(b bool) LoadUserArg {
	arg.staleOK = b
	return arg
}

func (arg LoadUserArg) GetNetContext() context.Context {
	if arg.netContext != nil {
		return arg.netContext
	}
	if ctx := arg.G().NetContext; ctx != nil {
		return ctx
	}
	return context.Background()
}

func (arg LoadUserArg) WithLoginContext(l LoginContext) LoadUserArg {
	arg.loginContext = l
	return arg
}

func (arg LoadUserArg) WithAbortIfSigchainUnchanged() LoadUserArg {
	arg.abortIfSigchainUnchanged = true
	return arg
}

func (arg LoadUserArg) WithForceReload() LoadUserArg {
	arg.forceReload = true
	return arg
}

func (arg *LoadUserArg) WithLogTag() context.Context {
	ctx := WithLogTag(arg.GetNetContext(), "LU")
	arg.netContext = ctx
	arg.SetGlobalContext(arg.G().CloneWithNetContextAndNewLogger(ctx))
	return ctx
}

func (arg *LoadUserArg) checkUIDName() error {
	if arg.uid.Exists() {
		return nil
	}

	if len(arg.name) == 0 && !arg.self {
		return fmt.Errorf("no username given to LoadUser")
	}

	if len(arg.name) > 0 && arg.self {
		return fmt.Errorf("If loading self, can't provide a username")
	}

	if !arg.self {
		return nil
	}

	if arg.uid = myUID(arg.G(), arg.loginContext); arg.uid.IsNil() {
		arg.name = arg.G().Env.GetUsername().String()
		if len(arg.name) == 0 {
			return SelfNotFoundError{msg: "could not find UID or username for self"}
		}
	}
	return nil
}

func (arg *LoadUserArg) resolveUID() (ResolveResult, error) {
	var rres ResolveResult
	if arg.uid.Exists() {
		return rres, nil
	}
	if len(arg.name) == 0 {
		// this won't happen anymore because check moved to
		// checkUIDName() func, but just in case
		return rres, fmt.Errorf("resolveUID: no uid or name")
	}

	if rres = arg.G().Resolver.ResolveWithBody(arg.name).FailOnDeleted(); rres.err != nil {
		return rres, rres.err
	}

	if rres.uid.IsNil() {
		return rres, fmt.Errorf("No resolution for name=%s", arg.name)
	}

	arg.uid = rres.uid
	return rres, nil
}

// after resolution, check if this is a self load
func (arg *LoadUserArg) checkSelf() {
	if arg.self {
		return
	}

	myuid := myUID(arg.G(), arg.loginContext)
	if myuid.Exists() && arg.uid.Exists() && myuid.Equal(arg.uid) {
		arg.self = true
	}
}

func LoadMe(arg LoadUserArg) (*User, error) {
	arg.self = true
	return LoadUser(arg)
}

func LoadMeByUID(ctx context.Context, g *GlobalContext, uid keybase1.UID) (*User, error) {
	return LoadMe(NewLoadUserByUIDArg(ctx, g, uid))
}

func LoadUser(arg LoadUserArg) (ret *User, err error) {

	ctx := arg.WithLogTag()
	defer arg.G().CTraceTimed(ctx, fmt.Sprintf("LoadUser(%s)", arg), func() error { return err })()

	var refresh bool

	if arg.G().VDL.DumpSiteLoadUser() {
		debug.PrintStack()
	}

	// Whatever the reply is, pass along our desired global context
	defer func() {
		if ret != nil {
			ret.SetGlobalContext(arg.G())
			if refresh {
				arg.G().NotifyRouter.HandleUserChanged(ret.GetUID())
			}
		}
	}()

	// make sure we have a uid or a name to load
	if err = arg.checkUIDName(); err != nil {
		return nil, err
	}

	arg.G().Log.CDebugf(ctx, "+ LoadUser(uid=%v, name=%v)", arg.uid, arg.name)

	// resolve the uid from the name, if necessary
	rres, err := arg.resolveUID()
	if err != nil {
		return nil, err
	}

	// check to see if this is a self load
	arg.checkSelf()

	arg.G().Log.CDebugf(ctx, "| resolved to %s", arg.uid)

	// We can get the user object's body from either the resolution result or
	// if it was plumbed through as a parameter.
	resolveBody := rres.body
	if resolveBody == nil {
		resolveBody = arg.resolveBody
	}

	// get sig hints from local db in order to populate during merkle leaf lookup
	// They might have already been loaded in.
	var sigHints *SigHints
	if sigHints = arg.sigHints; sigHints == nil {
		sigHints, err = LoadSigHints(ctx, arg.uid, arg.G())
		if err != nil {
			return nil, err
		}
	}

	// load user from local, remote
	ret, refresh, err = loadUser(ctx, arg.G(), arg.uid, resolveBody, sigHints, arg.forceReload, arg.merkleLeaf)
	if err != nil {
		return nil, err
	}

	ret.sigHints = sigHints

	// Match the returned User object to the Merkle tree. Also make sure
	// that the username queried for matches the User returned (if it
	// was indeed queried for)
	if err = ret.leaf.MatchUser(ret, arg.uid, rres.GetNormalizedQueriedUsername()); err != nil {
		return ret, err
	}

	if err = ret.LoadSigChains(ctx, &ret.leaf, arg.self); err != nil {
		return ret, err
	}

	if arg.abortIfSigchainUnchanged && ret.sigChain().wasFullyCached {
		return nil, nil
	}

	if ret.sigHints != nil && ret.sigHints.dirty {
		refresh = true
	}

	// Proactively cache fetches from remote server to local storage
	if e2 := ret.Store(ctx); e2 != nil {
		arg.G().Log.CWarningf(ctx, "Problem storing user %s: %s", ret.GetName(), e2)
	}

	if ret.HasActiveKey() {
		if err = ret.MakeIDTable(); err != nil {
			return ret, err
		}

		// Check that the user has self-signed only after we
		// consider revocations. See: https://github.com/keybase/go/issues/43
		if err = ret.VerifySelfSig(); err != nil {
			return ret, err
		}

	} else if !arg.publicKeyOptional {
		arg.G().Log.CDebugf(ctx, "No active key for user: %s", ret.GetUID())

		var emsg string
		if arg.self {
			emsg = "You don't have a public key; try `keybase pgp select` or `keybase pgp import` if you have a key; or `keybase pgp gen` if you don't"
		}
		err = NoKeyError{emsg}
	}

	return ret, err
}

func loadUser(ctx context.Context, g *GlobalContext, uid keybase1.UID, resolveBody *jsonw.Wrapper, sigHints *SigHints, force bool, leaf *MerkleUserLeaf) (*User, bool, error) {
	local, err := LoadUserFromLocalStorage(ctx, g, uid)
	var refresh bool
	if err != nil {
		g.Log.CWarningf(ctx, "Failed to load %s from storage: %s", uid, err)
	}

	if leaf == nil {
		leaf, err = lookupMerkleLeaf(ctx, g, uid, (local != nil), sigHints)
		if err != nil {
			return nil, refresh, err
		}
	}

	var f1, loadRemote bool

	if local == nil {
		g.Log.CDebugf(ctx, "| No local user stored for %s", uid)
		loadRemote = true
	} else if f1, err = local.CheckBasicsFreshness(leaf.idVersion); err != nil {
		return nil, refresh, err
	} else {
		loadRemote = !f1
		refresh = loadRemote
	}

	g.Log.CDebugf(ctx, "| Freshness: basics=%v; for %s", f1, uid)

	var ret *User
	if !loadRemote && !force {
		ret = local
	} else if ret, err = LoadUserFromServer(ctx, g, uid, resolveBody); err != nil {
		return nil, refresh, err
	}

	if ret == nil {
		return nil, refresh, nil
	}

	if leaf != nil {
		ret.leaf = *leaf
	}
	return ret, refresh, nil
}

func LoadUserFromLocalStorage(ctx context.Context, g *GlobalContext, uid keybase1.UID) (u *User, err error) {
	g.Log.CDebugf(ctx, "+ LoadUserFromLocalStorage(%s)", uid)
	jw, err := g.LocalDb.Get(DbKeyUID(DBUser, uid))
	if err != nil {
		return nil, err
	}

	if jw == nil {
		g.Log.CDebugf(ctx, "- loadUserFromLocalStorage(%s): Not found", uid)
		return nil, nil
	}

	g.Log.CDebugf(ctx, "| Loaded successfully")

	if u, err = NewUserFromLocalStorage(g, jw); err != nil {
		return nil, err
	}

	if u.id.NotEqual(uid) {
		err = fmt.Errorf("Bad lookup; uid mismatch: %s != %s", uid, u.id)
	}

	g.Log.CDebugf(ctx, "| Loaded username %s (uid=%s)", u.name, uid)
	g.Log.CDebugf(ctx, "- LoadUserFromLocalStorage(%s,%s)", u.name, uid)

	return
}

// LoadUserEmails returns emails for logged in user
func LoadUserEmails(g *GlobalContext) (emails []keybase1.Email, err error) {
	uid := g.GetMyUID()
	res, err := g.API.Get(APIArg{
		Endpoint:    "user/lookup",
		SessionType: APISessionTypeREQUIRED,
		Args: HTTPArgs{
			"uid": UIDArg(uid),
		},
	})
	if err != nil {
		return
	}
	var email string
	var isVerified int
	primary := res.Body.AtKey("them").AtKey("emails").AtKey("primary")
	email, err = primary.AtKey("email").GetString()
	if err != nil {
		return
	}
	isVerified, err = primary.AtKey("is_verified").GetInt()
	if err != nil {
		return
	}
	emails = []keybase1.Email{keybase1.Email{Email: email, IsVerified: isVerified == 1}}
	return
}

func LoadUserFromServer(ctx context.Context, g *GlobalContext, uid keybase1.UID, body *jsonw.Wrapper) (u *User, err error) {
	g.Log.CDebugf(ctx, "+ Load User from server: %s", uid)

	// Res.body might already have been preloaded a a result of a Resolve call earlier.
	if body == nil {
		res, err := g.API.Get(APIArg{
			Endpoint:    "user/lookup",
			SessionType: APISessionTypeNONE,
			Args: HTTPArgs{
				"uid":          UIDArg(uid),
				"load_deleted": B{true},
			},
			NetContext: ctx,
		})

		if err != nil {
			return nil, err
		}
		body = res.Body.AtKey("them")
	} else {
		g.Log.CDebugf(ctx, "| Skipped load; got user object previously")
	}

	if u, err = NewUserFromServer(g, body); err != nil {
		return u, err
	}
	g.Log.CDebugf(ctx, "- Load user from server: %s -> %s", uid, ErrToOk(err))

	return u, err
}

func myUID(g *GlobalContext, lctx LoginContext) keybase1.UID {
	if lctx != nil {
		return lctx.LocalSession().GetUID()
	}
	return g.GetMyUID()
}

func lookupMerkleLeaf(ctx context.Context, g *GlobalContext, uid keybase1.UID, localExists bool, sigHints *SigHints) (f *MerkleUserLeaf, err error) {
	if uid.IsNil() {
		err = fmt.Errorf("uid parameter for lookupMerkleLeaf empty")
		return
	}

	q := NewHTTPArgs()
	q.Add("uid", UIDArg(uid))

	f, err = g.MerkleClient.LookupUser(ctx, q, sigHints)
	if err == nil && f == nil && localExists {
		err = fmt.Errorf("User not found in server Merkle tree")
	}

	return
}

func lookupSigHintsAndMerkleLeaf(ctx context.Context, g *GlobalContext, uid keybase1.UID, localExists bool) (sigHints *SigHints, leaf *MerkleUserLeaf, err error) {
	defer g.CTrace(ctx, "lookupSigHintsAndMerkleLeaf", func() error { return err })()
	sigHints, err = LoadSigHints(ctx, uid, g)
	if err != nil {
		return nil, nil, err
	}

	leaf, err = lookupMerkleLeaf(ctx, g, uid, true, sigHints)
	if err != nil {
		return nil, nil, err
	}

	return sigHints, leaf, nil
}

// LoadUserPlusKeys loads user and keys for the given UID.  If `pollForKID` is provided, we'll request
// this user potentially twice: the first time can hit the cache for the UID, but will force a repoll
// unless the pollForKID is found for the user.  If pollForKID is empty, then just access the cache as
// normal.
func LoadUserPlusKeys(ctx context.Context, g *GlobalContext, uid keybase1.UID, pollForKID keybase1.KID) (keybase1.UserPlusKeys, error) {
	return g.GetUPAKLoader().LoadUserPlusKeys(ctx, uid, pollForKID)
}
