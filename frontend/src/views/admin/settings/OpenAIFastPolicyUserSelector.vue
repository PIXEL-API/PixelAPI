<template>
  <div ref="containerRef" class="relative w-full min-w-0">
    <div
      v-if="selectedUserIds.length > 0"
      class="mb-2 flex w-full flex-wrap gap-2"
    >
      <span
        v-for="userId in selectedUserIds"
        :key="userId"
        class="flex min-h-11 w-full min-w-0 items-center gap-1.5 rounded-md bg-gray-100 pl-3 text-sm text-gray-700 dark:bg-dark-600 dark:text-gray-200 sm:w-auto sm:max-w-full"
      >
        <template v-if="hydrationStates[userId] === 'loaded'">
          <span
            class="min-w-0 flex-1 truncate font-medium sm:max-w-64"
            :title="selectedUsers[userId]?.email"
          >
            {{ selectedUsers[userId]?.email }}
          </span>
          <span class="shrink-0 text-xs text-gray-400">#{{ userId }}</span>
          <span
            v-if="selectedUsers[userId]?.deleted"
            class="shrink-0 text-xs text-gray-400"
          >
            {{ t("admin.settings.openaiFastPolicy.userDeleted") }}
          </span>
        </template>
        <span
          v-else-if="hydrationStates[userId] === 'error'"
          class="min-w-0 flex-1 text-xs font-medium text-red-600 dark:text-red-400"
        >
          {{
            t("admin.settings.openaiFastPolicy.userLoadFailed", {
              id: userId,
            })
          }}
        </span>
        <span v-else class="min-w-0 flex-1 text-xs text-gray-500">
          {{ t("common.loading") }}
        </span>
        <button
          type="button"
          class="flex h-11 w-11 shrink-0 items-center justify-center rounded-r-md text-gray-400 transition-colors hover:bg-red-50 hover:text-red-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-red-900/20 dark:hover:text-red-400"
          :aria-label="t('admin.settings.openaiFastPolicy.removeUser')"
          :title="t('admin.settings.openaiFastPolicy.removeUser')"
          @click="removeUser(userId)"
        >
          <Icon name="x" size="xs" :stroke-width="2" />
        </button>
      </span>
    </div>

    <div class="relative w-full min-w-0">
      <Icon
        name="search"
        size="sm"
        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
      />
      <input
        v-model="searchQuery"
        type="search"
        autocomplete="off"
        role="combobox"
        aria-autocomplete="list"
        :aria-expanded="showDropdown && Boolean(searchQuery.trim())"
        :aria-label="
          t('admin.settings.openaiFastPolicy.userSearchPlaceholder')
        "
        class="input min-h-11 w-full min-w-0 pl-10"
        :placeholder="
          t('admin.settings.openaiFastPolicy.userSearchPlaceholder')
        "
        @input="debounceSearch"
        @focus="showDropdown = true"
        @keydown.esc="showDropdown = false"
      />
    </div>

    <div
      v-if="showDropdown && searchQuery.trim()"
      role="listbox"
      class="absolute left-0 right-0 z-50 mt-1 max-h-60 w-full min-w-0 overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700"
    >
      <div
        v-if="searchLoading"
        class="flex min-h-11 items-center px-4 py-2 text-sm text-gray-500 dark:text-gray-400"
      >
        {{ t("common.loading") }}
      </div>
      <div
        v-else-if="searchFailed"
        class="flex min-h-11 items-center px-4 py-2 text-sm text-red-600 dark:text-red-400"
      >
        {{ t("admin.settings.openaiFastPolicy.userSearchFailed") }}
      </div>
      <div
        v-else-if="availableResults.length === 0"
        class="flex min-h-11 items-center px-4 py-2 text-sm text-gray-500 dark:text-gray-400"
      >
        {{ t("admin.settings.openaiFastPolicy.userSearchEmpty") }}
      </div>
      <template v-else>
        <button
          v-for="user in availableResults"
          :key="user.id"
          type="button"
          role="option"
          aria-selected="false"
          class="flex min-h-11 w-full min-w-0 items-center justify-between gap-3 px-4 py-2 text-left text-sm transition-colors hover:bg-gray-100 focus-visible:bg-gray-100 focus-visible:outline-none dark:hover:bg-dark-600 dark:focus-visible:bg-dark-600"
          @click="selectUser(user)"
        >
          <span
            class="min-w-0 flex-1 truncate font-medium text-gray-900 dark:text-white"
          >
            {{ user.email }}
            <span
              v-if="user.deleted"
              class="ml-1 text-xs font-normal text-gray-400"
            >
              {{ t("admin.settings.openaiFastPolicy.userDeleted") }}
            </span>
          </span>
          <span class="shrink-0 text-xs text-gray-400">#{{ user.id }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { adminAPI } from "@/api/admin";
import type { SimpleUser } from "@/api/admin/usage";
import Icon from "@/components/icons/Icon.vue";

type HydrationState = "loading" | "loaded" | "error";

const props = defineProps<{
  modelValue: number[];
}>();

const emit = defineEmits<{
  "update:modelValue": [value: number[]];
}>();

const { t } = useI18n();
const containerRef = ref<HTMLElement | null>(null);
const searchQuery = ref("");
const searchResults = ref<SimpleUser[]>([]);
const searchLoading = ref(false);
const searchFailed = ref(false);
const showDropdown = ref(false);
const selectedUsers = ref<Record<number, SimpleUser>>({});
const hydrationStates = ref<Record<number, HydrationState>>({});
let searchTimer: ReturnType<typeof setTimeout> | null = null;
let searchSequence = 0;
let unmounted = false;

const selectedUserIds = computed(() =>
  Array.from(
    new Set(
      props.modelValue.filter((id) => Number.isInteger(id) && id > 0),
    ),
  ),
);

const availableResults = computed(() => {
  const selected = new Set(selectedUserIds.value);
  return searchResults.value
    .filter((user) => !selected.has(user.id))
    .sort(
      (a, b) => Number(Boolean(a.deleted)) - Number(Boolean(b.deleted)),
    );
});

function setHydrationState(userId: number, state: HydrationState): void {
  hydrationStates.value = {
    ...hydrationStates.value,
    [userId]: state,
  };
}

function clearPendingSearch(): void {
  if (searchTimer) {
    clearTimeout(searchTimer);
    searchTimer = null;
  }
  searchSequence += 1;
}

function debounceSearch(): void {
  clearPendingSearch();
  const query = searchQuery.value.trim();
  showDropdown.value = true;
  searchFailed.value = false;
  if (!query) {
    searchResults.value = [];
    searchLoading.value = false;
    return;
  }

  const sequence = searchSequence;
  searchTimer = setTimeout(async () => {
    searchLoading.value = true;
    try {
      const results = await adminAPI.usage.searchUsers(query);
      if (sequence === searchSequence) {
        searchResults.value = results;
      }
    } catch {
      if (sequence === searchSequence) {
        searchResults.value = [];
        searchFailed.value = true;
      }
    } finally {
      if (sequence === searchSequence) {
        searchLoading.value = false;
      }
    }
  }, 300);
}

function selectUser(user: SimpleUser): void {
  selectedUsers.value = { ...selectedUsers.value, [user.id]: user };
  setHydrationState(user.id, "loaded");
  emit("update:modelValue", [...selectedUserIds.value, user.id]);
  clearPendingSearch();
  searchQuery.value = "";
  searchResults.value = [];
  searchLoading.value = false;
  searchFailed.value = false;
  showDropdown.value = false;
}

function removeUser(userId: number): void {
  emit(
    "update:modelValue",
    selectedUserIds.value.filter((id) => id !== userId),
  );
}

async function hydrateUser(userId: number): Promise<void> {
  setHydrationState(userId, "loading");
  try {
    const user = await adminAPI.users.getById(userId, true);
    if (unmounted || !selectedUserIds.value.includes(userId)) return;

    selectedUsers.value = {
      ...selectedUsers.value,
      [userId]: {
        id: user.id,
        email: user.email,
        deleted: Boolean(user.deleted_at),
      },
    };
    setHydrationState(userId, "loaded");
  } catch {
    if (!unmounted && selectedUserIds.value.includes(userId)) {
      setHydrationState(userId, "error");
    }
  }
}

function hydrateSelectedUsers(userIds: number[]): void {
  for (const userId of userIds) {
    if (!hydrationStates.value[userId]) {
      void hydrateUser(userId);
    }
  }
}

function handleDocumentClick(event: MouseEvent): void {
  const target = event.target as Node | null;
  if (target && !containerRef.value?.contains(target)) {
    showDropdown.value = false;
  }
}

watch(selectedUserIds, hydrateSelectedUsers, { immediate: true });

onMounted(() => {
  document.addEventListener("click", handleDocumentClick);
});

onUnmounted(() => {
  unmounted = true;
  clearPendingSearch();
  document.removeEventListener("click", handleDocumentClick);
});
</script>
