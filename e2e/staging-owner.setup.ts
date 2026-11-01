import { prepareStagingSession } from "./staging-access";
export default async function setup() {
  await prepareStagingSession(true);
}
