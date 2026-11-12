import { chatOrderStatus, chatReferenceHref } from "./lib/chat-reference";
import type { ChatReference } from "./lib/chat";
export function ChatReferenceCard({
  reference,
  staff = false,
  preview = false,
}: {
  reference: ChatReference;
  staff?: boolean;
  preview?: boolean;
}) {
  const href = chatReferenceHref(reference, staff);
  const body = (
    <>
      {reference.kind === "product" && reference.image && (
        <img
          src={reference.image}
          alt=""
          loading="lazy"
          onError={(event) => {
            event.currentTarget.hidden = true;
          }}
        />
      )}
      <span>
        <small>{reference.kind === "product" ? "Товар" : "Заказ"}</small>
        <strong>{reference.label}</strong>
        {reference.kind === "order" && <span>{chatOrderStatus(reference.status)}</span>}
        {!reference.available && (
          <span>
            {reference.kind === "product" ? "Товар больше не доступен" : "Заказ больше не доступен"}
          </span>
        )}
      </span>
    </>
  );
  return reference.available && !preview ? (
    <a className="chat-reference-card" href={href}>
      {body}
    </a>
  ) : (
    <div className="chat-reference-card">{body}</div>
  );
}
