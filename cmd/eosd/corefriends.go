package main

// Friends, friend requests and blocks are the account core's (Social service): network-wide, one graph
// for every console. eosd only translates Among Us's PUID-addressed API to core account ids and back
// (accounts.go keeps the Epic/PUID <-> core account mapping, which is EOS-specific and lives in
// Postgres). ACCOUNT_GRPC_ADDR + ACCOUNT_INTERNAL_KEY name the core; without them social calls answer
// empty lists and refuse writes, because eosd must not grow a friends store of its own.
//
// Blocks: the core answers only "who I blocked", never "who blocked me", so blocked_by is always empty.
// Deny and unfriend are both RemoveFriend (the core ends pending and accepted alike).

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	accountv1 "github.com/openpak/account-api/openpak/account/v1"
)

const switchNamespace = "switch"

var errNoCore = errors.New("account core not configured")

// friendsCore is what eosd needs of the core, in account ids.
type friendsCore interface {
	AccountOf(ctx context.Context, baasUserID string) (string, error)
	Request(ctx context.Context, from, to string) error
	Accept(ctx context.Context, addressee, requester string) error
	Remove(ctx context.Context, a, b string) error
	Friends(ctx context.Context, a string) ([]string, error)
	Incoming(ctx context.Context, a string) ([]string, error)
	Outgoing(ctx context.Context, a string) ([]string, error)
	Block(ctx context.Context, blocker, blocked string) error
	Unblock(ctx context.Context, blocker, blocked string) error
	Blocks(ctx context.Context, a string) ([]string, error)
}

type grpcCore struct {
	links  accountv1.LinksClient
	social accountv1.SocialClient
}

// newCoreFromEnv dials the core, or returns nil when it is not configured.
func newCoreFromEnv() friendsCore {
	addr, key := os.Getenv("ACCOUNT_GRPC_ADDR"), os.Getenv("ACCOUNT_INTERNAL_KEY")
	if addr == "" || key == "" {
		log.Printf("[eos] ACCOUNT_GRPC_ADDR/ACCOUNT_INTERNAL_KEY unset: friends are unavailable")
		return nil
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any,
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-internal-key", key), 5*time.Second)
			defer cancel()
			return invoker(ctx, method, req, reply, cc, opts...)
		}))
	if err != nil {
		log.Printf("[eos] ERROR account core: %v", err)
		return nil
	}
	return &grpcCore{links: accountv1.NewLinksClient(conn), social: accountv1.NewSocialClient(conn)}
}

func (c *grpcCore) AccountOf(ctx context.Context, baas string) (string, error) {
	r, err := c.links.GetLinkBySubject(ctx, &accountv1.GetLinkBySubjectRequest{Namespace: switchNamespace, SubjectId: baas})
	if err != nil {
		return "", err
	}
	if r.GetAccountId() == "" {
		return "", fmt.Errorf("no account for the link")
	}
	return r.GetAccountId(), nil
}

func (c *grpcCore) Request(ctx context.Context, from, to string) error {
	_, err := c.social.RequestFriend(ctx, &accountv1.RequestFriendRequest{RequesterId: from, AddresseeId: to})
	return err
}

func (c *grpcCore) Accept(ctx context.Context, addressee, requester string) error {
	_, err := c.social.AcceptFriend(ctx, &accountv1.AcceptFriendRequest{AddresseeId: addressee, RequesterId: requester})
	return err
}

func (c *grpcCore) Remove(ctx context.Context, a, b string) error {
	_, err := c.social.RemoveFriend(ctx, &accountv1.RemoveFriendRequest{AccountId: a, OtherId: b})
	return err
}

func (c *grpcCore) Friends(ctx context.Context, a string) ([]string, error) {
	r, err := c.social.ListFriends(ctx, &accountv1.ListFriendsRequest{AccountId: a})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range r.GetFriends() {
		out = append(out, f.GetAccountId())
	}
	return out, nil
}

func (c *grpcCore) Incoming(ctx context.Context, a string) ([]string, error) {
	r, err := c.social.ListIncomingRequests(ctx, &accountv1.ListIncomingRequestsRequest{AccountId: a})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range r.GetRequesters() {
		out = append(out, f.GetAccountId())
	}
	return out, nil
}

func (c *grpcCore) Outgoing(ctx context.Context, a string) ([]string, error) {
	r, err := c.social.ListOutgoingRequests(ctx, &accountv1.ListOutgoingRequestsRequest{AccountId: a})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range r.GetAddressees() {
		out = append(out, f.GetAccountId())
	}
	return out, nil
}

func (c *grpcCore) Block(ctx context.Context, blocker, blocked string) error {
	_, err := c.social.Block(ctx, &accountv1.BlockRequest{BlockerId: blocker, BlockedId: blocked})
	return err
}

func (c *grpcCore) Unblock(ctx context.Context, blocker, blocked string) error {
	_, err := c.social.Unblock(ctx, &accountv1.UnblockRequest{BlockerId: blocker, BlockedId: blocked})
	return err
}

func (c *grpcCore) Blocks(ctx context.Context, a string) ([]string, error) {
	r, err := c.social.ListBlocks(ctx, &accountv1.ListBlocksRequest{AccountId: a})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range r.GetBlocked() {
		out = append(out, f.GetAccountId())
	}
	return out, nil
}
