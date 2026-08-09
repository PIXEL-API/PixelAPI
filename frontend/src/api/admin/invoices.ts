import { apiClient } from "../client";
import type { BasePaginationResponse, InvoiceRequest } from "@/types";

export interface IssueInvoicePayload {
  admin_note?: string;
}

export interface RejectInvoicePayload {
  reason: string;
  admin_note?: string;
}

export const adminInvoicesAPI = {
  list(params?: {
    page?: number;
    page_size?: number;
    status?: string;
    user_id?: number;
    keyword?: string;
  }) {
    return apiClient.get<BasePaginationResponse<InvoiceRequest>>(
      "/admin/invoices",
      { params },
    );
  },

  get(id: number) {
    return apiClient.get<InvoiceRequest>(`/admin/invoices/${id}`);
  },

  issue(id: number, data: IssueInvoicePayload) {
    return apiClient.post<InvoiceRequest>(`/admin/invoices/${id}/issue`, data);
  },

  reject(id: number, data: RejectInvoicePayload) {
    return apiClient.post<InvoiceRequest>(`/admin/invoices/${id}/reject`, data);
  },
};

export default adminInvoicesAPI;
