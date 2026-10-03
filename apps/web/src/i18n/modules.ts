import account from "./locales/es/account";
import auth from "./locales/es/auth";
import common from "./locales/es/common";
import dashboard from "./locales/es/dashboard";
import events from "./locales/es/events";
import labels from "./locales/es/labels";
import live from "./locales/es/live";
import maps from "./locales/es/maps";
import nav from "./locales/es/nav";
import plates from "./locales/es/plates";
import protectedImages from "./locales/es/protected";
import search from "./locales/es/search";
import settings from "./locales/es/settings";
import status from "./locales/es/status";

/** Spanish modules are the key list. A new module is added here and as a file in every language. */
export const sourceModules = {
  account,
  auth,
  common,
  dashboard,
  events,
  labels,
  live,
  maps,
  nav,
  plates,
  protected: protectedImages,
  search,
  settings,
  status,
};

export type MessageKey = {
  [Module in keyof typeof sourceModules]: `${Module & string}.${keyof (typeof sourceModules)[Module] & string}`;
}[keyof typeof sourceModules];
