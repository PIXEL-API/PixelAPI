<template>
  <span
    v-if="label"
    :data-api-key-badge="type"
    :title="label"
    :class="[
      'inline-flex max-w-40 shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-semibold leading-5',
      badgeClass,
    ]"
  >
    <svg
      v-if="type === 'recommended'"
      aria-hidden="true"
      class="h-3.5 w-3.5 shrink-0"
      fill="none"
      stroke="currentColor"
      viewBox="0 0 24 24"
      stroke-width="2"
    >
      <path stroke-linecap="round" stroke-linejoin="round" d="m5 12 4 4L19 6" />
    </svg>
    <svg
      v-else-if="type === 'constrained' || type === 'unavailable'"
      aria-hidden="true"
      class="h-3.5 w-3.5 shrink-0"
      fill="none"
      stroke="currentColor"
      viewBox="0 0 24 24"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M12 9v4m0 4h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"
      />
    </svg>
    <span class="truncate">{{ label }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { ApiKeyGroupBadgeType, GroupScope } from "@/types";

interface Props {
  type?: ApiKeyGroupBadgeType | null;
  text?: string | null;
  scope?: GroupScope;
}

const props = withDefaults(defineProps<Props>(), {
  type: "hidden",
  text: "",
  scope: "public",
});

const { t } = useI18n();

const label = computed(() => {
  if (props.scope === "user_private" || props.type === "hidden") return "";

  switch (props.type) {
    case "recommended":
      return t("groups.apiKeyBadge.recommended");
    case "constrained":
      return t("groups.apiKeyBadge.constrained");
    case "unavailable":
      return t("groups.apiKeyBadge.unavailable");
    case "custom":
      return props.text?.trim() ?? "";
    default:
      return "";
  }
});

const badgeClass = computed(() => {
  switch (props.type) {
    case "recommended":
      return "bg-emerald-50 text-emerald-600 dark:bg-emerald-950/40 dark:text-emerald-400";
    case "constrained":
      return "bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-400";
    case "unavailable":
      return "bg-red-50 text-red-600 dark:bg-red-950/40 dark:text-red-400";
    case "custom":
      return "bg-blue-50 text-blue-700 dark:bg-blue-950/40 dark:text-blue-300";
    default:
      return "";
  }
});
</script>
