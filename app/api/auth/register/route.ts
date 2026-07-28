import { headers } from "next/headers";
import { normalizeRussianPhone } from "../../../../lib/phone";
import {
  createSession,
  hashPassword,
  passwordIsAcceptable,
} from "../../../../lib/server/auth";
import { getPostgres } from "../../../../lib/server/postgres";

export const runtime = "nodejs";

type RegistrationBody = {
  fullName?: string;
  phone?: string;
  email?: string;
  password?: string;
  accountType?: string;
  companyName?: string;
  inn?: string;
  kpp?: string;
  legalAddress?: string;
  consent?: boolean;
};

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export async function POST(request: Request) {
  let body: RegistrationBody;
  try {
    body = (await request.json()) as RegistrationBody;
  } catch {
    return Response.json({ error: "Некорректные данные формы" }, { status: 400 });
  }

  const fullName = body.fullName?.trim() ?? "";
  const email = body.email?.trim().toLowerCase() ?? "";
  const phone = normalizeRussianPhone(body.phone ?? "");
  const password = body.password ?? "";
  const accountType = body.accountType === "wholesale" ? "wholesale" : "retail";
  const inn = (body.inn ?? "").replace(/\D/g, "");
  const companyName = body.companyName?.trim() ?? "";
  const kpp = (body.kpp ?? "").replace(/\D/g, "");
  const legalAddress = body.legalAddress?.trim() ?? "";

  if (fullName.length < 2 || fullName.length > 120) {
    return Response.json({ error: "Укажите имя покупателя" }, { status: 400 });
  }
  if (!phone) {
    return Response.json({ error: "Проверьте номер телефона" }, { status: 400 });
  }
  if (!emailPattern.test(email) || email.length > 254) {
    return Response.json({ error: "Проверьте электронную почту" }, { status: 400 });
  }
  if (!passwordIsAcceptable(password)) {
    return Response.json(
      { error: "Пароль должен содержать не менее 10 символов, букву и цифру" },
      { status: 400 },
    );
  }
  if (!body.consent) {
    return Response.json(
      { error: "Необходимо принять условия обработки данных" },
      { status: 400 },
    );
  }
  if (
    accountType === "wholesale" &&
    (companyName.length < 2 || ![10, 12].includes(inn.length))
  ) {
    return Response.json(
      { error: "Для оптового аккаунта укажите организацию и корректный ИНН" },
      { status: 400 },
    );
  }
  if (kpp && kpp.length !== 9) {
    return Response.json({ error: "КПП должен содержать 9 цифр" }, { status: 400 });
  }

  try {
    const sql = getPostgres();
    const passwordHash = await hashPassword(password);
    let customerId = 0;

    await sql.begin(async (transaction) => {
      const duplicate = await transaction`
        SELECT email, phone
        FROM customers
        WHERE LOWER(email) = ${email} OR phone = ${phone}
        LIMIT 1
      `;
      if (duplicate[0]) {
        throw new Error("ACCOUNT_EXISTS");
      }

      const customers = await transaction`
        INSERT INTO customers (
          email, phone, password_hash, full_name, account_type,
          wholesale_status, consent_at
        )
        VALUES (
          ${email}, ${phone}, ${passwordHash}, ${fullName}, ${accountType},
          ${accountType === "wholesale" ? "pending" : "not_requested"},
          CURRENT_TIMESTAMP
        )
        RETURNING id
      `;
      customerId = Number(customers[0].id);

      if (accountType === "wholesale") {
        const existingOrganizations = await transaction`
          SELECT id FROM organizations WHERE inn = ${inn} LIMIT 1
        `;
        let organizationId: number;

        if (existingOrganizations[0]) {
          organizationId = Number(existingOrganizations[0].id);
        } else {
          const organizations = await transaction`
            INSERT INTO organizations (name, inn, kpp, legal_address)
            VALUES (${companyName}, ${inn}, ${kpp || null}, ${legalAddress})
            RETURNING id
          `;
          organizationId = Number(organizations[0].id);
        }

        await transaction`
          INSERT INTO organization_members (organization_id, customer_id, role)
          VALUES (${organizationId}, ${customerId}, 'buyer')
        `;
      }
    });

    const userAgent = (await headers()).get("user-agent");
    await createSession(customerId, userAgent);
    return Response.json({ ok: true, accountType }, { status: 201 });
  } catch (error) {
    if (error instanceof Error && error.message === "ACCOUNT_EXISTS") {
      return Response.json(
        { error: "Аккаунт с таким телефоном или email уже существует" },
        { status: 409 },
      );
    }
    console.error("Registration failed", error);
    return Response.json(
      { error: "Не удалось создать аккаунт. Попробуйте позднее" },
      { status: 500 },
    );
  }
}
