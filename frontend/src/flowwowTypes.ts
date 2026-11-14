export type FlowwowChannel = {
  enabled: boolean;
  mode: "create" | "link";
  existingId: string;
  article?: string;
  category: { id: string; name: string };
  attributes?: Record<string, unknown>;
};
export const blankFlowwow = (): FlowwowChannel => ({
  enabled: false,
  mode: "create",
  existingId: "",
  category: { id: "", name: "" },
});
export type FlowwowField = {
  field: string;
  name: string;
  type: string;
  required: boolean;
  dependsOn?: string[];
  minValue?: number;
  maxValue?: number;
  maxDecimalPlaces?: number;
  values?: Array<{ id: number | string; name: string }>;
  measure?: Array<{ id: number }>;
};
