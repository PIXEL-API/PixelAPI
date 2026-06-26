import type { BasePaginationResponse } from '@/types'

export type ActivityType = 'consumption_lottery'
export type ActivityStatus = 'draft' | 'active' | 'paused' | 'ended'
export type ActivityMetric = 'api_cost_amount' | 'api_request_count'
export type ActivityPeriodType = 'fixed_range' | 'today' | 'rolling_days' | 'campaign'
export type ActivityTicketMode = 'fixed' | 'proportional' | 'tiered'
export type ActivityTierMode = 'highest' | 'cumulative'
export type ActivityPrizeType = 'balance' | 'points' | 'load_factor_credits' | 'manual'
export type ActivityWinnerStatus = 'pending_claim' | 'pending_delivery' | 'delivered' | 'rejected' | 'expired'
export type ActivityClaimStatus = 'not_required' | 'pending' | 'submitted'
export type ActivityPublicParticipantCountMode = 'off' | 'fuzzy' | 'exact'

export interface ActivityRuleTier {
  threshold: number
  tickets: number
}

export interface ActivityRuleConfig {
  metric: ActivityMetric
  period_type: ActivityPeriodType
  period_start_at?: string | null
  period_end_at?: string | null
  rolling_days?: number
  threshold: number
  ticket_mode: ActivityTicketMode
  fixed_tickets?: number
  unit_amount?: number
  tickets_per_unit?: number
  max_tickets_per_user?: number
  tier_mode?: ActivityTierMode
  tiers?: ActivityRuleTier[]
}

export interface ActivityClaimField {
  key: string
  label: string
  required: boolean
  type?: string
}

export interface ActivityDisplayConfig {
  public_participant_count?: ActivityPublicParticipantCountMode
  [key: string]: unknown
}

export interface ActivityPrize {
  id?: number
  campaign_id?: number
  name: string
  description?: string | null
  prize_type: ActivityPrizeType
  amount: number
  quantity: number
  weight: number
  requires_claim_info: boolean
  claim_fields: ActivityClaimField[]
  enabled: boolean
  sort_order: number
  created_at?: string
  updated_at?: string
}

export interface ActivityProgress {
  metric_type: ActivityMetric
  metric_value: number
  ticket_count: number
  next_threshold?: number | null
  next_tickets?: number | null
  period_start_at: string
  period_end_at: string
  draw_at?: string | null
  joined: boolean
  joined_tickets: number
  joined_at?: string | null
}

export interface ActivityWinnerPublic {
  id: number
  campaign_id: number
  campaign_name: string
  prize_name: string
  prize_type: ActivityPrizeType
  prize_amount: number
  masked_user: string
  created_at: string
}

export interface ActivityPublicStats {
  participant_count_mode: ActivityPublicParticipantCountMode
  participant_count?: number
  participant_count_bucket?: string
}

export interface ActivityCampaign {
  id: number
  type: ActivityType
  name: string
  description?: string | null
  cover_url?: string | null
  status: ActivityStatus
  starts_at: string
  ends_at: string
  draw_at?: string | null
  timezone: string
  public_enabled: boolean
  sort_order: number
  rule_config: ActivityRuleConfig
  display_config: ActivityDisplayConfig
  prizes?: ActivityPrize[]
  user_progress?: ActivityProgress | null
  public_stats?: ActivityPublicStats | null
  yesterday_winners?: ActivityWinnerPublic[]
  recent_winners?: ActivityWinnerPublic[]
  created_at?: string
  updated_at?: string
}

export interface ActivityWinner {
  id: number
  campaign_id: number
  campaign_name?: string
  draw_id: number
  prize_id?: number | null
  user_id: number
  user_email?: string
  user_username?: string
  prize_name: string
  prize_type: ActivityPrizeType
  prize_amount: number
  claim_fields?: ActivityClaimField[]
  ticket_count: number
  masked_user: string
  status: ActivityWinnerStatus
  claim_status: ActivityClaimStatus
  claim_info?: Record<string, unknown>
  claim_submitted_at?: string | null
  delivered_at?: string | null
  admin_note?: string | null
  created_at: string
  updated_at: string
}

export interface ActivityDrawResult {
  draw_id: number
  campaign_id: number
  campaign_name?: string
  total_users: number
  total_tickets: number
  winner_count: number
  winners: ActivityWinner[]
}

export interface ActivityDrawSummary {
  id: number
  draw_at: string
  snapshot_start_at: string
  snapshot_end_at: string
  status: string
  total_users: number
  total_tickets: number
  winner_count: number
  executed_at: string
}

export interface ActivityCampaignStats {
  campaign_id: number
  campaign_name: string
  status: ActivityStatus
  period_start_at: string
  period_end_at: string
  draw_at?: string | null
  joined_user_count: number
  joined_ticket_count: number
  joined_metric_total: number
  average_tickets_per_user: number
  average_metric_value: number
  max_ticket_count: number
  max_metric_value: number
  first_joined_at?: string | null
  last_joined_at?: string | null
  enabled_prize_count: number
  prize_total_quantity: number
  winner_count: number
  pending_claim_count: number
  pending_delivery_count: number
  delivered_count: number
  rejected_count: number
  expired_count: number
  claim_submitted_count: number
  pending_action_count: number
  drawn: boolean
  can_run_draw: boolean
  draw_block_reason?: string
  no_participant_warning: boolean
  latest_draw?: ActivityDrawSummary | null
}

export interface ActivityCampaignPayload {
  type: ActivityType
  name: string
  description?: string | null
  cover_url?: string | null
  status: ActivityStatus
  starts_at: string
  ends_at: string
  draw_at?: string | null
  timezone: string
  public_enabled: boolean
  sort_order: number
  rule_config: ActivityRuleConfig
  display_config: ActivityDisplayConfig
  prizes: ActivityPrize[]
}

export type ActivityCampaignPage = BasePaginationResponse<ActivityCampaign>
export type ActivityWinnerPage = BasePaginationResponse<ActivityWinner>
