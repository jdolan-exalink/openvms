import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { setLocale } from "@/i18n";

window.scrollTo = () => {};
setLocale("es");
afterEach(() => setLocale("es"));
