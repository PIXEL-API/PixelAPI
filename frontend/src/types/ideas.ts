export interface IdeaTag {
  id: number;
  name: string;
  slug: string;
  status: string;
  sort_order: number;
  usage_count: number;
}

export interface IdeaRevision {
  id: number;
  post_id: number;
  revision_no: number;
  title: string;
  summary: string;
  body: string;
  body_hash: string;
  moderation_status: string;
  moderation_reason?: string;
  published_at?: string;
  created_by: number;
  created_at: string;
}

export interface IdeaPost {
  id: number;
  author_user_id: number;
  author_name: string;
  current_revision_id: number;
  status: string;
  published_at?: string;
  like_count: number;
  favorite_count: number;
  view_count: number;
  created_at: string;
  updated_at: string;
  revision?: IdeaRevision;
  tags?: IdeaTag[];
  can_edit: boolean;
  can_reward: boolean;
  liked: boolean;
  favorited: boolean;
}

export interface IdeaReward {
  id: number;
  payer_user_id: number;
  recipient_user_id: number;
  post_id: number;
  revision_id: number;
  asset_type: 'balance' | 'points';
  amount: number;
  status: string;
  created_at: string;
}

export interface IdeaAsset {
  id: number;
  post_id: number;
  revision_id: number;
  file_name: string;
  mime_type: string;
  size_bytes: number;
  sha256?: string;
  status: string;
  uploader_user_id: number;
  created_at: string;
}

export interface IdeaReport {
  id: number;
  post_id: number;
  post_title?: string;
  reporter_user_id: number;
  reason: string;
  detail: string;
  status: string;
  resolution?: string;
  created_at: string;
}
