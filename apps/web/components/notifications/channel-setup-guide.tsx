"use client";

import { ChevronRight, Info } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { InlineCode } from "@/components/common/code-block";
import type { ChannelType } from "@/lib/api";
import { CHANNEL_TYPE_INFO } from "@/lib/channels";
import { PUBLIC_API_URL } from "@/lib/config";

function A({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="font-medium text-brand underline-offset-2 hover:underline"
    >
      {children}
    </a>
  );
}

function Steps({ children }: { children: React.ReactNode }) {
  return <ol className="grid list-decimal gap-1.5 pl-5 marker:text-muted-foreground">{children}</ol>;
}

function Note({ children }: { children: React.ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

/** Short setup steps per channel type, condensed from docs/notifications.md. */
function GuideBody({ type }: { type: ChannelType }) {
  switch (type) {
    case "telegram":
      return (
        <>
          <Steps>
            <li>
              In Telegram, open <strong>@BotFather</strong>, send <InlineCode>/newbot</InlineCode> and copy the bot
              token (<InlineCode>123456:ABC…</InlineCode>).
            </li>
            <li>
              Open a chat with your new bot and send <InlineCode>/start</InlineCode>.
            </li>
            <li>
              Find your chat ID: open <InlineCode>api.telegram.org/bot&lt;TOKEN&gt;/getUpdates</InlineCode> and read{" "}
              <InlineCode>message.chat.id</InlineCode>, or save the channel with any chat ID (e.g.{" "}
              <InlineCode>0</InlineCode>) and send <InlineCode>/chatid</InlineCode> to the bot — it replies with the
              chat ID and your user ID.
            </li>
            <li>For a group, add the bot to the group, use the group&apos;s (negative) ID and list who may approve.</li>
          </Steps>
          <Note>
            Polling works on a laptop without a public URL. Use one channel per bot — two pollers on the same bot steal
            each other&apos;s updates.
          </Note>
        </>
      );
    case "slack":
      return (
        <>
          <Steps>
            <li>
              Create an app at <A href="https://api.slack.com/apps">api.slack.com/apps</A> and turn on{" "}
              <strong>Incoming Webhooks</strong>.
            </li>
            <li>
              Choose <strong>Add New Webhook to Workspace</strong>, pick a channel and paste the URL below. Messages get
              Approve… / Reject… link buttons.
            </li>
            <li>
              Optional, to decide inside Slack: under <strong>Interactivity &amp; Shortcuts</strong> set the Request URL
              to <InlineCode className="break-all">{PUBLIC_API_URL}/v1/integrations/slack/interactions</InlineCode>,
              then add the <strong>Signing Secret</strong> (Basic Information) and the member IDs (
              <InlineCode>U…</InlineCode>) of the people who may decide.
            </li>
          </Steps>
          <Note>
            Instead of a webhook you can use a bot token (<InlineCode>xoxb-…</InlineCode>, scope{" "}
            <InlineCode>chat:write</InlineCode>) and a channel ID. Buttons need a public HTTPS URL.
          </Note>
        </>
      );
    case "discord":
      return (
        <>
          <Steps>
            <li>
              Simplest: <strong>Server Settings → Integrations → Webhooks → New Webhook</strong>, then paste the URL.
              Messages get Approve… / Reject… links.
            </li>
            <li>
              For buttons inside Discord: create an application at{" "}
              <A href="https://discord.com/developers/applications">discord.com/developers</A>, add a bot under{" "}
              <strong>Bot</strong>, copy its token and invite it with <em>Send Messages</em>.
            </li>
            <li>
              Copy the <strong>Application ID</strong> and <strong>Public Key</strong> (General Information), the
              channel ID (Developer Mode → Copy Channel ID) and the user IDs who may decide.
            </li>
            <li>
              Set <strong>Interactions Endpoint URL</strong> to{" "}
              <InlineCode className="break-all">{PUBLIC_API_URL}/v1/integrations/discord/interactions</InlineCode>.
            </li>
          </Steps>
          <Note>Buttons need a public HTTPS URL.</Note>
        </>
      );
    case "ntfy":
      return (
        <>
          <Steps>
            <li>
              Install the <A href="https://ntfy.sh">ntfy</A> app.
            </li>
            <li>Subscribe to the topic you enter below.</li>
          </Steps>
          <Note>
            On ntfy.sh anyone who knows the topic name can read it — use a long random topic or a protected topic with
            an access token. The Approve / Reject actions only work if your phone can reach the staq.ly API.
          </Note>
        </>
      );
    case "email":
      return (
        <>
          <Steps>
            <li>Enter your SMTP server and credentials (an API-key login of your mail provider works).</li>
            <li>Add up to 20 recipients. Messages carry Approve / Reject links and a dashboard link.</li>
          </Steps>
          <Note>
            STARTTLS (port 587) is the default; use TLS for port 465. “None” is only for a trusted local relay —
            credentials are never sent in plaintext except to localhost.
          </Note>
        </>
      );
  }
}

export function ChannelSetupGuide({ type, defaultOpen }: { type: ChannelType; defaultOpen?: boolean }) {
  return (
    <Collapsible defaultOpen={defaultOpen} className="group/guide rounded-xl border bg-muted/40">
      <CollapsibleTrigger className="flex min-h-11 w-full items-center gap-2 px-3 text-left text-sm font-medium">
        <Info className="size-4 shrink-0 text-brand" aria-hidden />
        <span className="flex-1">How to set up {CHANNEL_TYPE_INFO[type].label}</span>
        <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]/guide:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent className="grid gap-2 px-3 pb-3 text-[13px] leading-relaxed" data-testid="setup-guide">
        <GuideBody type={type} />
      </CollapsibleContent>
    </Collapsible>
  );
}
