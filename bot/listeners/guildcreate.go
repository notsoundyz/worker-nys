package listeners

import (
	"context"
	"fmt"
	"github.com/TicketsBot/common/sentry"
	"github.com/TicketsBot/worker"
	"github.com/TicketsBot/worker/bot/blacklist"
	"github.com/TicketsBot/worker/bot/customisation"
	"github.com/TicketsBot/worker/bot/dbclient"
	"github.com/TicketsBot/worker/bot/metrics/statsd"
	"github.com/rxdn/gdl/gateway/payloads/events"
	"github.com/rxdn/gdl/objects/auditlog"
	"github.com/rxdn/gdl/objects/channel/embed"
	"github.com/rxdn/gdl/objects/guild"
	"github.com/rxdn/gdl/permission"
	"github.com/rxdn/gdl/rest"
	"time"
)

// Fires when we receive a guild
func OnGuildCreate(worker *worker.Context, e events.GuildCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*6) // TODO: Propagate context
	defer cancel()

	// check if guild is blacklisted
	if blacklist.IsGuildBlacklisted(e.Guild.Id) {
		if err := worker.LeaveGuild(e.Guild.Id); err != nil {
			sentry.Error(err)
		}

		return
	}

	if time.Now().Sub(e.JoinedAt) < time.Minute {
		statsd.Client.IncrementKey(statsd.KeyJoins)

		sendIntroMessage(ctx, worker, e.Guild, e.Guild.OwnerId)

		// find who invited the bot
		if inviter := getInviter(worker, e.Guild.Id); inviter != 0 && inviter != e.Guild.OwnerId {
			sendIntroMessage(ctx, worker, e.Guild, inviter)
		}

		if err := dbclient.Client.GuildLeaveTime.Delete(ctx, e.Guild.Id); err != nil {
			sentry.Error(err)
		}

		// Add roles with Administrator permission as bot admins by default
		for _, role := range e.Roles {
			// Don't add @everyone role, even if it has Administrator
			if role.Id == e.Guild.Id {
				continue
			}

			if permission.HasPermissionRaw(role.Permissions, permission.Administrator) {
				if err := dbclient.Client.RolePermissions.AddAdmin(ctx, e.Guild.Id, role.Id); err != nil { // TODO: Bulk
					sentry.Error(err)
				}
			}
		}
	}
}

func sendIntroMessage(ctx context.Context, worker *worker.Context, guild guild.Guild, userId uint64) {
	// Create DM channel
	channel, err := worker.CreateDM(userId)
	if err != nil { // User probably has DMs disabled
		return
	}

	msg := embed.NewEmbed().
		SetTitle("Tickets").
		SetDescription("Thank you for inviting Tickets to your server! Below is a quick guide on setting the bot up, please don't hesitate to contact us in our [support server](https://discord.gg/nys) if you need any assistance!").
		SetColor(customisation.GetColourOrDefault(ctx, guild.Id, customisation.Green)).
		AddField("Setup", "You can setup the bot using `/setup`, or you can use the [dashboard](https://dash.nysrp.xyz) which has additional options", false).
		AddField("Reaction Panels", fmt.Sprintf("Reaction panels are a commonly used feature of the bot. You can read about them [here](https://dash.nysrp.xyz/panels), or create one on [the dashboard](https://dashboard.nysrp.xyz/manage/%d/panels)", guild.Id), false).
		AddField("Adding Staff", "To make staff able to answer tickets, you must let the bot know about them first. You can do this through\n`/addsupport [@User / @Role]` and `/addadmin [@User / @Role]`. Administrators can change the settings of the bot and access the dashboard.", false).
		AddField("Tags", "Tags are predefined tickets of text which you can access through a simple command. You can learn more about them [here](https://nysrp.xyz/tags).", false).
		AddField("Claiming", "Tickets can be claimed by your staff such that other staff members cannot also reply to the ticket. You can learn more about claiming [here](https://nysrp.xyz/claiming).", false).
		AddField("Additional Support", "If you are still confused, we welcome you to our [support server](https://discord.gg/nys). Cheers.", false)

	_, _ = worker.CreateMessageEmbed(channel.Id, msg)
}

func getInviter(worker *worker.Context, guildId uint64) (userId uint64) {
	data := rest.GetGuildAuditLogData{
		ActionType: auditlog.EventBotAdd,
		Limit:      50,
	}

	auditLog, err := worker.GetGuildAuditLog(guildId, data)
	if err != nil {
		sentry.Error(err) // prob perms
		return
	}

	for _, entry := range auditLog.Entries {
		if entry.ActionType != auditlog.EventBotAdd || entry.TargetId != worker.BotId {
			continue
		}

		userId = entry.UserId
		break
	}

	return
}
