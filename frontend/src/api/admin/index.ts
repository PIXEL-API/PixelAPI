/**
 * Admin API barrel export
 * Centralized exports for all admin API modules
 */

import dashboardAPI from "./dashboard";
import usersAPI from "./users";
import groupsAPI from "./groups";
import accountsAPI from "./accounts";
import proxiesAPI from "./proxies";
import redeemAPI from "./redeem";
import promoAPI from "./promo";
import announcementsAPI from "./announcements";
import conversationsAPI from "./conversations";
import settingsAPI from "./settings";
import systemAPI from "./system";
import subscriptionsAPI from "./subscriptions";
import usageAPI from "./usage";
import geminiAPI from "./gemini";
import antigravityAPI from "./antigravity";
import grokAPI from "./grok";
import userAttributesAPI from "./userAttributes";
import opsAPI from "./ops";
import clusterAPI from "./cluster";
import errorPassthroughAPI from "./errorPassthrough";
import dataManagementAPI from "./dataManagement";
import apiKeysAPI from "./apiKeys";
import scheduledTestsAPI from "./scheduledTests";
import backupAPI from "./backup";
import tlsFingerprintProfileAPI from "./tlsFingerprintProfile";
import channelsAPI from "./channels";
import channelMonitorAPI from "./channelMonitor";
import channelMonitorTemplateAPI from "./channelMonitorTemplate";
import adminPaymentAPI from "./payment";
import revenueAPI from "./revenue";
import adminInvoicesAPI from "./invoices";
import affiliatesAPI from "./affiliates";
import accountSharePoliciesAPI from "./accountSharePolicies";
import riskControlAPI from "./riskControl";

/**
 * Unified admin API object for convenient access
 */
export const adminAPI = {
  dashboard: dashboardAPI,
  users: usersAPI,
  groups: groupsAPI,
  accounts: accountsAPI,
  proxies: proxiesAPI,
  redeem: redeemAPI,
  promo: promoAPI,
  announcements: announcementsAPI,
  conversations: conversationsAPI,
  settings: settingsAPI,
  system: systemAPI,
  subscriptions: subscriptionsAPI,
  usage: usageAPI,
  gemini: geminiAPI,
  antigravity: antigravityAPI,
  grok: grokAPI,
  userAttributes: userAttributesAPI,
  ops: opsAPI,
  cluster: clusterAPI,
  errorPassthrough: errorPassthroughAPI,
  dataManagement: dataManagementAPI,
  apiKeys: apiKeysAPI,
  scheduledTests: scheduledTestsAPI,
  backup: backupAPI,
  tlsFingerprintProfiles: tlsFingerprintProfileAPI,
  channels: channelsAPI,
  channelMonitor: channelMonitorAPI,
  channelMonitorTemplate: channelMonitorTemplateAPI,
  payment: adminPaymentAPI,
  revenue: revenueAPI,
  invoices: adminInvoicesAPI,
  affiliates: affiliatesAPI,
  accountSharePolicies: accountSharePoliciesAPI,
  riskControl: riskControlAPI,
};

export {
  dashboardAPI,
  usersAPI,
  groupsAPI,
  accountsAPI,
  proxiesAPI,
  redeemAPI,
  promoAPI,
  announcementsAPI,
  conversationsAPI,
  settingsAPI,
  systemAPI,
  subscriptionsAPI,
  usageAPI,
  geminiAPI,
  antigravityAPI,
  grokAPI,
  userAttributesAPI,
  opsAPI,
  clusterAPI,
  errorPassthroughAPI,
  dataManagementAPI,
  apiKeysAPI,
  scheduledTestsAPI,
  backupAPI,
  tlsFingerprintProfileAPI,
  channelsAPI,
  channelMonitorAPI,
  channelMonitorTemplateAPI,
  adminPaymentAPI,
  revenueAPI,
  adminInvoicesAPI,
  affiliatesAPI,
  accountSharePoliciesAPI,
  riskControlAPI,
};

export default adminAPI;

// Re-export types used by components
export type { BalanceHistoryItem } from "./users";
export type {
  ErrorPassthroughRule,
  CreateRuleRequest,
  UpdateRuleRequest,
} from "./errorPassthrough";
export type { BackupAgentHealth, DataManagementConfig } from "./dataManagement";
export type {
  TLSFingerprintProfile,
  CreateProfileRequest,
  UpdateProfileRequest,
} from "./tlsFingerprintProfile";
export type {
  RevenueSummary,
  RevenueSummaryParams,
} from "./revenue";
export type { AccountSharePolicy } from "./accountSharePolicies";
export type {
  ClusterSummary,
  ClusterInstance,
  ClusterTaskLease,
  ClusterOperation,
  ClusterCacheScope,
} from "./cluster";
