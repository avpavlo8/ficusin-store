export type CustomerPushChannel = "orders" | "chat";
export function notificationMutation(
  channel: CustomerPushChannel,
  enabled: boolean,
  subscription: { endpoint: string; toJSON: () => unknown },
) {
  return {
    url:
      channel === "orders"
        ? `/api/v1/push/${enabled ? "subscribe" : "unsubscribe"}`
        : "/api/v1/chat/notifications",
    method: channel === "chat" && !enabled ? "DELETE" : "POST",
    body: enabled ? subscription.toJSON() : { endpoint: subscription.endpoint },
  };
}
