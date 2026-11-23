// Isolated in-memory API for reviewing the real procurement React UI locally.
// No credentials, production requests, external integrations or database access.
export function procurementPreview() {
  const now = "2026-11-14T15:20:00Z";
  const suppliers = [
    {
      id: 1,
      name: "Голландия",
      kind: "international",
      countryCode: "NL",
      taxId: "",
      kpp: "",
      defaultCurrency: "EUR",
      active: true,
      createdAt: now,
    },
    {
      id: 2,
      name: "ООО «ТК Ярославский»",
      kind: "domestic",
      countryCode: "RU",
      taxId: "",
      kpp: "",
      defaultCurrency: "RUB",
      active: true,
      createdAt: now,
    },
  ];
  const samples = [
    ["Олива Европейская D12", 1, 29, 24, 6, 1, "8128009"],
    ["Азалия D9", 0, 25, 20, 10, 1, "8364397"],
    ["Бонсай Кармона D15", 0, 25, 20, 10, 1, "8352013"],
    ["Алоказия Регал Шилдс D10", 0, 24, 18, 1, 2, ""],
    ["Каламондин D12", 5, 26, 18, 6, 1, "8144565"],
    ["Бонсай Сагереция чайная D15", 0, 20, 16, 2, 1, "8441913"],
    ["Фиттония D9", 0, 21, 16, 1, 1, "101605"],
    ["Лиметта Росса D14", 0, 18, 15, 3, 1, "325365"],
    ["Бамбук Лаки Драцена Сандериана", 1, 19, 14, 1, 1, "93394"],
    ["Бокарнея Нолина D6", 6, 18, 14, 14, 1, "9132"],
    ["Фикус Микрокарпа D10", 0, 17, 13, 1, 1, ""],
    ["Бонсай Кармона D10", 0, 14, 12, 2, 1, "8351992"],
    ["Бонсай Зантоксилум D10", 7, 14, 10, 10, 1, "8354469"],
    ["Пальмы микс (Арека и Хамедорея) D10", 0, 11, 9, 1, 0, ""],
    ["Бонсай Кармона D16 (белое кашпо)", 0, 1, 2, 1, 0, ""],
  ];
  const recommendations = samples.map(
    (
      [
        name,
        balance,
        totalSales,
        suggestedQty,
        orderMultiple,
        supplierId,
        supplierArticle,
      ],
      index,
    ) => ({
      aliasId: index + 1,
      sabyId: `PREVIEW-${index + 1}`,
      name,
      balance,
      balanceKnown: true,
      observedStockDays: 42,
      inStockDays: balance > 0 ? 39 : 20,
      balanceAsOf: now,
      totalSales,
      grossSales: totalSales + (index === 0 ? 1 : 0),
      returns: index === 0 ? 1 : 0,
      supplierId,
      supplierArticle,
      dutchName: name,
      potDiameterCm: Number(name.match(/D(\d+)/)?.[1] || 12),
      heightCm: 35,
      lastUnitPrice: 5.3,
      incoming: 0,
      availability: "available",
      siteSales: index === 14 ? 1 : 0,
      sabySales: Math.floor(totalSales * 0.2),
      wbSales: Math.floor(totalSales * 0.3),
      ozonSales:
        totalSales -
        Math.floor(totalSales * 0.2) -
        Math.floor(totalSales * 0.3) -
        (index === 14 ? 1 : 0),
      customerRequests: index === 14 ? 1 : 0,
      staffRequests: 0,
      openRequests: index === 14 ? 1 : 0,
      allocatedRequests: 0,
      minimumOrderQty: 1,
      orderMultiple,
      suggestedQty,
      demandQty: Math.ceil(totalSales * 0.75),
      needBeforeIncoming: suggestedQty,
      uncoveredQty: suggestedQty,
      quantityKnown: true,
      roundingExplanation: `Кратность упаковки ${orderMultiple} шт. Рекомендовано ${suggestedQty} шт.`,
      dailySales: totalSales / 60,
      daysOfCover: Math.round(balance / (totalSales / 60)),
      lastSaleAt: "2026-11-12",
      lastOrderedAt: "2026-10-08",
      status: "recommended",
      reason: `За 60 дн. продано ${totalSales} шт., на остатке ${balance} шт.; закупаем запас на 45 дн.`,
    }),
  );
  const settings = {
    economics: { monthlyFixedCostsRub: 300000, channels: {} },
    version: 1,
    defaultExchangeRate: 105,
    trolleyCostCurrency: 0,
    trolleyCostRub: 63700,
    trolleyVolumeCm3: 1646000,
    trolleyFillRatio: 0.85,
    returnLossRate: 0.05,
    marketplaceCostRate: 0,
    taxRate: 0.08,
    reserveRate: 0,
    packageRub: 100,
    priceChangeThreshold: 0.1,
    marketplaceStrikeMarkup: 0.3,
    recommendationDays: 60,
    targetCoverDays: 45,
    retailMarkupMultiplier: 2.1,
    roundPrices: true,
    marketplaceLogisticsPerCm: 0,
  };
  const orders = [
    {
      id: 19,
      supplierId: 1,
      supplierName: suppliers[0].name,
      title: "Октябрьская закупка",
      orderNumber: "8208515",
      documentNumber: "8208515",
      sourceKind: "invoice",
      currency: "EUR",
      status: "received",
      lines: 3,
      units: 64,
      total: 339.2,
      unmatched: 0,
      failedActions: 2,
      createdAt: "2026-10-08T10:00:00Z",
    },
    {
      id: 20,
      supplierId: 1,
      supplierName: suppliers[0].name,
      title: "Отменённая закупка",
      orderNumber: "8208516",
      documentNumber: "",
      sourceKind: "manual",
      currency: "EUR",
      status: "cancelled",
      lines: 1,
      units: 6,
      total: 31.8,
      unmatched: 1,
      failedActions: 1,
      createdAt: "2026-10-09T10:00:00Z",
    },
  ];
  const requests = [
    {
      id: 1,
      kind: "customer",
      source: "customer_order",
      customerName: "Клиент (пример)",
      customerOrderId: 0,
      sabyId: "PREVIEW-15",
      requestedName: samples[14][0],
      quantity: 1,
      allocatedQuantity: 0,
      remainingQuantity: 1,
      status: "open",
      notes: "Предзаказ клиента — демонстрационный пример",
      dueDate: "",
      assignee: "",
      allocations: [],
      history: [],
      createdAt: now,
      updatedAt: now,
    },
  ];
  const channels = ["saby", "site", "wb", "ozon"].map((channel, i) => ({
    channel,
    status: "ok",
    freshness: "fresh",
    lastSuccessAt: now,
    latestSale: "2026-11-14",
    lastError: "",
    rowsSynced: [116, 113, 702, 420][i],
    rowsUnlinked: channel === "ozon" ? 1 : 0,
    rowsLinked: 100,
    periodFrom: "2026-09-15",
    periodTo: "2026-11-14",
  }));
  let salesResolved = false;
  let salesIgnored = false;
  const procurement = {
    summary: {
      openOrders: 0,
      unresolvedAliases: 0,
      availabilityChecks: 0,
      openRequests: 1,
    },
    integrations: { saby: true, wb: true, ozon: true },
    settings,
    suppliers,
    orders,
    documents: [
      {
        id: 701,
        supplierId: 1,
        supplierName: suppliers[0].name,
        orderId: 19,
        fileName: "demo-invoice-8208515.pdf",
        parserKind: "holland_packing_list",
        parseStatus: "parsed",
        arithmeticStatus: "ok",
        documentNumber: "8208515",
        documentDate: "2026-10-08",
        currency: "EUR",
        lines: 3,
        units: 64,
        productSubtotal: 339.2,
        packageTotal: 0,
        documentTotal: 339.2,
        calculatedTotal: 339.2,
        parseError: "",
        revisionNo: 1,
        superseded: false,
        createdAt: "2026-10-08T10:00:00Z",
      },
    ],
    review: [],
    requests,
    availability: [],
    recommendations,
    salesSync: channels,
    integrationHealth: ["saby", "wb", "ozon"].map((channel) => ({
      channel,
      configured: true,
      lastError: channel === "ozon" ? "Не удалось загрузить продажи" : "",
      lastSuccessAt: now,
    })),
    integrationSync: [],
  };
  function samplePDF() {
    const stream =
      "BT /F1 16 Tf 40 760 Td (LOCAL PREVIEW - Invoice 8208515) Tj 0 -28 Td (Procurement 19 - demonstration document) Tj ET";
    const objects = [
      "<< /Type /Catalog /Pages 2 0 R >>",
      "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
      "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
      "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
      `<< /Length ${Buffer.byteLength(stream)} >>\nstream\n${stream}\nendstream`,
    ];
    let pdf = "%PDF-1.4\n";
    const offsets = [0];
    objects.forEach((object, i) => {
      offsets.push(Buffer.byteLength(pdf));
      pdf += `${i + 1} 0 obj\n${object}\nendobj\n`;
    });
    const start = Buffer.byteLength(pdf);
    pdf +=
      `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n` +
      offsets
        .slice(1)
        .map((value) => `${String(value).padStart(10, "0")} 00000 n \n`)
        .join("") +
      `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${start}\n%%EOF`;
    return Buffer.from(pdf);
  }
  const files = new Map([[701, samplePDF()]]);
  const drafts = [];
  const details = new Map();
  function orderDetail(order) {
    if (details.has(order.id)) return details.get(order.id);
    const lines = recommendations.slice(0, order.lines).map((rec, i) => ({
      id: order.id * 100 + i,
      supplierCategory: "Растения",
      rawName: rec.name,
      sabyId: rec.sabyId,
      sabyName: rec.name,
      sabyCode: rec.supplierArticle,
      supplierArticle: rec.supplierArticle,
      matchStatus: "confirmed",
      orderedQuantity: rec.suggestedQty,
      invoicedQuantity: rec.suggestedQty,
      reconciliationStatus: "matched",
      invoiceExcluded: false,
      invoiceRawName: rec.name,
      invoiceSupplierArticle: rec.supplierArticle,
      unitPrice: 5.3,
      expectedUnitPrice: 5.3,
      lineTotal: rec.suggestedQty * 5.3,
      potDiameterCm: rec.potDiameterCm,
      heightCm: 35,
      packageCount: 4,
      unitsPerPackage: rec.orderMultiple,
      trolleyCount: 0,
      loadUnits: 1,
      currentRetailRub: 1490,
      proposedRetailRub: 1590,
      unitCostRub: 700,
      priceChangeNeeded: false,
      customerRequest: false,
      comparisonMismatch: false,
      comparisonAccepted: true,
      comparisonNote: "",
    }));
    const item = (id, lineId, channel, status) => ({
      id,
      lineId,
      productName: lines[0]?.rawName || "Олива D12",
      productCode: "8128009",
      channel,
      externalArticle: "8128009",
      displayArticle: "8128009",
      oldValue: 1490,
      newValue: 1590,
      status,
      errorMessage:
        status === "failed"
          ? channel === "wb"
            ? "Не найден nmID товара в Wildberries"
            : "Ошибка старой попытки"
          : "",
    });
    const batches =
      order.status === "received"
        ? [
            {
              id: 1,
              kind: "receipt",
              status: "cancelled",
              createdAt: "2026-10-08T11:00:00Z",
              items: [item(1, lines[0]?.id, "saby_receipt", "failed")],
            },
            {
              id: 2,
              kind: "receipt",
              status: "completed",
              createdAt: "2026-10-08T12:00:00Z",
              items: [item(2, lines[0]?.id, "saby_receipt", "completed")],
            },
            {
              id: 3,
              kind: "prices",
              status: "failed",
              createdAt: "2026-10-08T13:00:00Z",
              items: [item(3, lines[0]?.id, "wb", "failed")],
            },
          ]
        : [];
    const detail = {
      order,
      costs: {
        exchangeRate: 105,
        currentDefaultExchangeRate: 105,
        trolleyCostCurrency: 0,
        trolleyCostRub: 0,
        deliveryToMoscowRub: 0,
        deliveryToRyazanRub: 0,
      },
      validation: {
        canCalculate: false,
        canPrepareActions: false,
        canPrepareReceipt: false,
        blockers: [],
        arithmeticMismatch: 0,
        comparisonMismatch: 0,
        missingDimensions: 0,
        missingLoadUnits: 0,
        invalidLines: 0,
        unmatched: 0,
        trolleyCount: 0,
        expectedTrolleyRub: 0,
        allocatedTrolleyRub: 0,
        expectedRyazanRub: 0,
        allocatedRyazanRub: 0,
      },
      lines,
      batches,
    };
    details.set(order.id, detail);
    return detail;
  }
  function route(path, method, body, query) {
    if (path === "/api/v1/auth/me")
      return {
        user: {
          id: 1,
          fullName: "Локальный просмотр",
          phone: "",
          email: "",
          isAdmin: true,
        },
      };
    if (path === "/api/v1/admin/dashboard")
      return {
        user: { fullName: "Локальный просмотр" },
        role: "owner",
        permissions: [
          "dashboard.read",
          "procurement.read",
          "procurement.edit",
          "integrations.edit",
          "catalog.delete",
        ],
        dashboard: {
          products: 0,
          variants: 0,
          orders: 0,
          customers: 0,
          wholesalePending: 0,
          recentOrders: [],
        },
      };
    if (path === "/api/v1/admin/procurement") {
      orders.sort(
        (a, b) => b.createdAt.localeCompare(a.createdAt) || b.id - a.id,
      );
      return procurement;
    }
    if (path === "/api/v1/admin/settings")
      return { definitions: [], values: {} };
    if (path === "/api/v1/admin/procurement/documents" && method === "POST") {
      if (!body.file?.bytes || !body.supplierId)
        return { status: 400, error: "Выберите поставщика и PDF-файл." };
      const supplier = suppliers.find(
        (item) => item.id === Number(body.supplierId),
      );
      if (!supplier) return { status: 400, error: "Поставщик не найден" };
      let order = orders.find((item) => item.id === Number(body.orderId));
      if (order && ["received", "cancelled"].includes(order.status))
        return {
          status: 409,
          error:
            "Закупка закрыта. Верните её в работу перед загрузкой новой версии.",
        };
      const bytes = Buffer.from(body.file.bytes);
      const duplicateId = [...files].find(([, existing]) =>
        existing.equals(bytes),
      )?.[0];
      if (duplicateId)
        return {
          duplicate: true,
          order: orders.find(
            (item) =>
              item.id ===
              procurement.documents.find((doc) => doc.id === duplicateId)
                ?.orderId,
          ),
        };
      if (!order) {
        order = {
          id: Math.max(0, ...orders.map((item) => item.id)) + 1,
          supplierId: supplier.id,
          supplierName: supplier.name,
          title: body.file.name,
          orderNumber: "",
          documentNumber: "",
          sourceKind:
            supplier.kind === "domestic" ? "payment_invoice" : "invoice",
          currency: supplier.defaultCurrency,
          status: "invoice_received",
          lines: 0,
          units: 0,
          total: 0,
          unmatched: 0,
          createdAt: new Date().toISOString(),
        };
        orders.unshift(order);
        procurement.summary.openOrders++;
      }
      if (body.expectedArrivalDate)
        order.expectedArrivalDate = body.expectedArrivalDate;
      const previous = procurement.documents.filter(
        (doc) => doc.orderId === order.id,
      );
      previous.forEach((doc) => {
        doc.superseded = true;
      });
      const id =
        Math.max(700, ...procurement.documents.map((item) => item.id)) + 1;
      files.set(id, bytes);
      procurement.documents.push({
        id,
        supplierId: supplier.id,
        supplierName: supplier.name,
        orderId: order.id,
        fileName: body.file.name,
        parserKind:
          supplier.kind === "domestic"
            ? "domestic_payment_invoice"
            : "holland_packing_list",
        parseStatus: "review",
        arithmeticStatus: "unknown",
        documentNumber: "",
        documentDate: "",
        currency: supplier.defaultCurrency,
        lines: 0,
        units: 0,
        productSubtotal: 0,
        packageTotal: 0,
        documentTotal: 0,
        calculatedTotal: 0,
        parseError:
          "Локальный просмотр: PDF сохранён. Разбор строк и проверка сумм выполняются рабочим сервером.",
        revisionNo: previous.length + 1,
        superseded: false,
        createdAt: new Date().toISOString(),
      });
      const detail = orderDetail(order);
      detail.validation.blockers = [
        "Локальный просмотр: файл сохранён; разбор строк требует рабочего сервера.",
      ];
      return { duplicate: false, order };
    }
    if (path === "/api/v1/admin/procurement/plan-drafts") {
      if (method === "POST") {
        const draft = {
          ...body,
          id: Date.now(),
          createdAt: now,
          updatedAt: now,
        };
        drafts.push(draft);
        return { draft };
      }
      return { drafts };
    }
    const draftId = path.match(/\/plan-drafts\/(\d+)$/)?.[1];
    if (draftId) {
      const index = drafts.findIndex((item) => item.id === Number(draftId));
      if (index < 0) return { status: 404, error: "Черновик не найден" };
      if (method === "PUT")
        Object.assign(drafts[index], body, {
          updatedAt: new Date().toISOString(),
        });
      if (method === "DELETE") {
        drafts.splice(index, 1);
        return {};
      }
      return { draft: drafts[index] };
    }
    if (path === "/api/v1/admin/procurement/plans/preview")
      return {
        lines: (body.items || []).map((line) => ({
          purchase: line.expectedUnitPrice * 105,
          cost: line.expectedUnitPrice * 105 + 100,
          retail:
            Math.ceil(((line.expectedUnitPrice * 105 + 100) * 2.1) / 10) * 10,
        })),
      };
    if (path === "/api/v1/admin/procurement/plans" && method === "POST") {
      const supplier =
        suppliers.find((item) => item.id === body.supplierId) || suppliers[0];
      const order = {
        id: Math.max(0, ...orders.map((item) => item.id)) + 1,
        supplierId: supplier.id,
        supplierName: supplier.name,
        title: body.title || "Новая закупка",
        orderNumber: "ЛОКАЛЬНЫЙ-" + (orders.length + 1),
        documentNumber: "",
        sourceKind: body.sourceKind || "manual",
        currency: supplier.defaultCurrency,
        status: "ordered",
        lines: body.items?.length || 0,
        units: (body.items || []).reduce(
          (sum, line) => sum + line.packageCount * line.unitsPerPackage,
          0,
        ),
        total: (body.items || []).reduce(
          (sum, line) => sum + line.quantity * line.expectedUnitPrice,
          0,
        ),
        unmatched: 0,
        createdAt: now,
      };
      const detail = orderDetail(order);
      const template = detail.lines[0] || {};
      detail.lines = (body.items || []).map((line, index) => ({
        ...template,
        id: order.id + index,
        rawName: line.rawName,
        sabyName: line.rawName,
        sabyId: line.sabyId,
        supplierCategory: line.category,
        supplierArticle: line.supplierArticle,
        orderedQuantity: line.quantity,
        invoicedQuantity: null,
        invoiceRawName: "",
        invoiceSupplierArticle: "",
        reconciliationStatus: "planned",
        comparisonAccepted: false,
        packageCount: line.packageCount,
        unitsPerPackage: line.unitsPerPackage,
        potDiameterCm: line.potDiameterCm,
        heightCm: line.heightCm,
        expectedUnitPrice: line.expectedUnitPrice,
        unitPrice: line.expectedUnitPrice,
        lineTotal: line.quantity * line.expectedUnitPrice,
      }));
      Object.assign(detail.costs, body.costs);
      orders.unshift(order);
      for (const line of body.items || []) {
        const item = recommendations.find(
          (item) => item.sabyId === line.sabyId,
        );
        if (item) {
          item.incoming += line.quantity;
          item.suggestedQty = Math.max(0, item.suggestedQty - line.quantity);
          if (!item.suggestedQty) item.status = "already_ordered";
        }
      }
      procurement.summary.openOrders++;
      return { order };
    }
    const arrivalOrderId = path.match(/\/orders\/(\d+)\/status$/)?.[1];
    if (
      method === "PATCH" &&
      arrivalOrderId &&
      typeof body.expectedArrivalDate === "string"
    ) {
      const order = orders.find((item) => item.id === Number(arrivalOrderId));
      if (!order) return { status: 404, error: "Закупка не найдена" };
      order.expectedArrivalDate = body.expectedArrivalDate;
      return orderDetail(order);
    }
    const orderId = path.match(/\/orders\/(\d+)$/)?.[1];
    if (orderId) {
      const order = orders.find((item) => item.id === Number(orderId));
      if (!order) return { status: 404, error: "Закупка не найдена" };
      if (method === "PATCH") Object.assign(order, body);
      return orderDetail(order);
    }
    if (
      path === "/api/v1/admin/procurement/recommendations/supplier" &&
      method === "PUT"
    ) {
      const item = recommendations.find((item) => item.sabyId === body.sabyId);
      if (item)
        Object.assign(item, {
          supplierId: body.supplierId,
          supplierArticle: "",
        });
      return {};
    }
    if (path === "/api/v1/admin/procurement/exclusions" && method === "PUT") {
      const ids = new Set(body.sabyIds || [body.sabyId]);
      const items = recommendations.filter((item) => ids.has(item.sabyId));
      if (items.length !== ids.size)
        return { status: 404, error: "Товар не найден" };
      for (const item of items) {
        item.hiddenFromRestock = body.excluded;
        item.status =
          body.excluded && !item.customerRequests
            ? "excluded"
            : item.suggestedQty || !item.incoming
              ? "recommended"
              : "already_ordered";
      }
      return {};
    }
    if (
      path === "/api/v1/admin/procurement/availability" &&
      method === "PATCH"
    ) {
      const item = recommendations.find((item) => item.sabyId === body.sabyId);
      if (item) {
        item.availability = body.status;
        procurement.availability = recommendations
          .filter((entry) =>
            ["check", "temporarily_unavailable", "discontinued"].includes(
              entry.availability,
            ),
          )
          .map((entry) => ({
            ...entry,
            supplierName: suppliers.find(
              (supplier) => supplier.id === entry.supplierId,
            )?.name,
            status: entry.availability,
            due: true,
          }));
        procurement.summary.availabilityChecks =
          procurement.availability.length;
      }
      return {};
    }
    if (path === "/api/v1/admin/procurement/nomenclature/balances")
      return {
        items: recommendations.map((item) => ({
          sabyId: item.sabyId,
          balance: item.balance,
          defaults: [],
          seenAt: now,
        })),
      };
    if (
      path === "/api/v1/admin/procurement/nomenclature" ||
      path === "/api/v1/admin/procurement/sales/nomenclature"
    )
      return {
        items: recommendations.map((item, index) => ({
          ...item,
          variantId: index + 1,
          code: item.supplierArticle,
          price: 1490,
          supplierLinked: true,
        })),
      };
    if (path === "/api/v1/admin/procurement/sales/unlinked")
      return {
        items:
          salesResolved ||
          query.get("channel") !== "ozon" ||
          (salesIgnored && query.get("ignored") !== "1")
            ? []
            : [
                {
                  ignored: salesIgnored,
                  channel: "ozon",
                  externalId: "preview-olive",
                  article: "preview-olive",
                  name: "Олива Европейская D12",
                  days: 3,
                  units: 7,
                  grossRub: 4900,
                  lastSale: "2026-11-12",
                },
              ],
      };
    if (path === "/api/v1/admin/procurement/sales/ignore" && method === "PUT") {
      salesIgnored = body.ignored;
      const channel = channels.find((item) => item.channel === body.channel);
      if (channel) channel.rowsUnlinked = salesIgnored ? 0 : 1;
      return { ok: true };
    }
    if (path === "/api/v1/admin/procurement/sales/link" && method === "POST") {
      const candidate = recommendations[body.variantId - 1];
      if (!candidate)
        return { status: 400, error: "Выберите товар справочника" };
      salesResolved = true;
      const channel = channels.find((item) => item.channel === body.channel);
      if (channel) channel.rowsUnlinked = 0;
      return {
        link: {
          ...body,
          sabyId: candidate.sabyId,
          sabyName: candidate.name,
          linkedRows: 1,
          linkedUnits: 7,
          remaining: 0,
        },
      };
    }
    if (path === "/api/v1/admin/procurement/pricing/preview")
      return {
        retailRub: 1490,
        quotes: {
          site: {
            priceRub: 1490,
            breakEvenRub: 800,
            contributionRub: 600,
            missing: [],
          },
        },
      };
    if (path === "/api/v1/admin/procurement/requests" && method === "POST") {
      const request = {
        ...body,
        id: Date.now(),
        status: "open",
        allocatedQuantity: 0,
        remainingQuantity: body.quantity,
        createdAt: now,
        allocations: [],
        history: [],
      };
      requests.push(request);
      procurement.summary.openRequests++;
      return { request };
    }
    const requestId = path.match(/\/requests\/(\d+)$/)?.[1];
    if (requestId && method === "PATCH") {
      const request = requests.find((item) => item.id === Number(requestId));
      if (request) Object.assign(request, body);
      return { request };
    }
    if (path === "/api/v1/admin/procurement/products") return { items: [] };
    if (path === "/api/v1/admin/chat/unread") return { unread: 0 };
    if (path === "/api/v1/analytics/config") return { enabled: false };
    if (path === "/api/v1/push/key") return { enabled: false, publicKey: "" };
    if (method === "GET") return {};
    return {
      status: 501,
      error:
        "Этот сценарий требует настоящего сервера. В локальном просмотре внешние действия отключены.",
    };
  }
  return {
    name: "local-procurement-preview",
    transformIndexHtml() {
      return [
        {
          tag: "div",
          attrs: {
            role: "note",
            style:
              "position:fixed;bottom:10px;right:12px;z-index:9999;padding:6px 12px;border-radius:6px;background:#285e43;color:white;font:12px/1.5 system-ui;box-shadow:0 2px 8px #0002;pointer-events:none",
          },
          children:
            "Локальный просмотр · демоданные · изменения только на этом стенде",
          injectTo: "body",
        },
      ];
    },
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        const parsed = new URL(req.url || "/", "http://localhost");
        const path = parsed.pathname;
        if (!path.startsWith("/api/")) return next();
        try {
          const sourceId = path.match(/\/documents\/(\d+)\/source\.pdf$/)?.[1];
          if (sourceId && req.method === "GET") {
            const bytes = files.get(Number(sourceId));
            if (!bytes) {
              res.statusCode = 404;
              res.end("Document not found");
              return;
            }
            res.setHeader("Content-Type", "application/pdf");
            res.setHeader(
              "Content-Disposition",
              `inline; filename="procurement-document-${sourceId}.pdf"`,
            );
            res.end(bytes);
            return;
          }
          const chunks = [];
          let size = 0;
          for await (const chunk of req) {
            size += chunk.length;
            if (size > 21 * 1024 * 1024) throw new Error("File too large");
            chunks.push(chunk);
          }
          const raw = Buffer.concat(chunks);
          const contentType = req.headers["content-type"] || "";
          let body = {};
          if (contentType.startsWith("multipart/form-data")) {
            const boundary = contentType.match(
              /boundary=(?:"([^";]+)"|([^;]+))/,
            );
            if (!boundary) throw new Error("Missing boundary");
            for (const part of raw
              .toString("latin1")
              .split("--" + (boundary[1] || boundary[2]))) {
              const split = part.indexOf("\r\n\r\n");
              if (split < 0) continue;
              const header = part.slice(0, split),
                name = header.match(/name="([^"]+)"/)?.[1];
              if (!name) continue;
              const value = Buffer.from(
                part.slice(split + 4).replace(/\r\n$/, ""),
                "latin1",
              );
              const filename = header.match(/filename="([^"]+)"/)?.[1];
              body[name] = filename
                ? {
                    name: Buffer.from(filename, "latin1").toString("utf8"),
                    bytes: value,
                  }
                : value.toString("utf8");
            }
          } else if (raw.length) body = JSON.parse(raw.toString("utf8"));
          const result = route(
            path,
            req.method || "GET",
            body,
            parsed.searchParams,
          );
          res.statusCode = result.status || 200;
          res.setHeader("Content-Type", "application/json; charset=utf-8");
          res.setHeader("Cache-Control", "no-store");
          res.end(JSON.stringify(result));
        } catch {
          res.statusCode = 400;
          res.end(
            JSON.stringify({ error: "Не удалось обработать локальный запрос" }),
          );
        }
      });
    },
  };
}
