package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// EscalationPoller watches a Redis sorted set for expired SOS escalation deadlines
// and advances to the next tier (or calls 911) when a tier times out.
//
// The sorted set key is "sos:escalations".  Each member encodes the SOS state as
// "<sosID>:<wearerID>:<currentTier>" and its score is the Unix timestamp at which
// the next escalation should fire.
//
// When an SOS is cancelled, the Cancel handler stores a short-lived key
// "sos:cancelled:<sosID>" in Redis so the poller can skip it cheaply.
type EscalationPoller struct {
	rdb    *redis.Client
	db     SOSRepository
	caller Caller
}

// NewEscalationPoller creates an EscalationPoller.
func NewEscalationPoller(rdb *redis.Client, db SOSRepository, caller Caller) *EscalationPoller {
	return &EscalationPoller{rdb: rdb, db: db, caller: caller}
}

// Run starts the polling loop, checking for expired escalations every interval.
// It stops when ctx is cancelled.
func (p *EscalationPoller) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processExpired(ctx)
		}
	}
}

// processExpired fetches all expired escalation members and handles each one.
func (p *EscalationPoller) processExpired(ctx context.Context) {
	now := fmt.Sprintf("%d", time.Now().Unix())
	members, err := p.rdb.ZRangeByScore(ctx, "sos:escalations", &redis.ZRangeBy{
		Min: "0",
		Max: now,
	}).Result()
	if err != nil {
		return
	}
	for _, member := range members {
		p.escalate(ctx, member)
	}
}

// escalate handles a single expired escalation entry.
// member format: "<sosID>:<wearerID>:<currentTier>"
func (p *EscalationPoller) escalate(ctx context.Context, member string) {
	parts := strings.SplitN(member, ":", 3)
	if len(parts) != 3 {
		p.rdb.ZRem(ctx, "sos:escalations", member)
		return
	}
	sosID, wearerID, tierStr := parts[0], parts[1], parts[2]
	currentTier, err := strconv.Atoi(tierStr)
	if err != nil {
		p.rdb.ZRem(ctx, "sos:escalations", member)
		return
	}

	// Remove from queue regardless of outcome.
	p.rdb.ZRem(ctx, "sos:escalations", member)

	// Skip if this SOS has been cancelled.
	cancelled, _ := p.rdb.Exists(ctx, "sos:cancelled:"+sosID).Result()
	if cancelled > 0 {
		return
	}

	// Try next tier.
	nextTier := currentTier + 1
	contact, err := p.db.GetContactByTier(ctx, wearerID, nextTier)
	if err != nil {
		return
	}

	if contact == nil {
		// All tiers exhausted — check auto_911.
		settings, err := p.db.GetSOSSettings(ctx, wearerID)
		if err == nil && settings != nil && settings.Auto911 {
			_ = p.caller.Call(ctx, "911")
			_ = p.db.LogEscalation(ctx, sosID, "911", 0)
		}
		return
	}

	// Call next-tier contact.
	_ = p.caller.Call(ctx, contact.Phone)
	_ = p.db.LogEscalation(ctx, sosID, contact.Phone, contact.Tier)

	// Re-add with the new tier's deadline.
	if contact.TimeoutSec > 0 {
		deadline := float64(time.Now().Add(time.Duration(contact.TimeoutSec) * time.Second).Unix())
		newMember := fmt.Sprintf("%s:%s:%d", sosID, wearerID, nextTier)
		p.rdb.ZAdd(ctx, "sos:escalations", redis.Z{Score: deadline, Member: newMember})
	}
}
