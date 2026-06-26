import { apiClient } from "./client";
import type {
  BasePaginationResponse,
  InvoiceEligibleSource,
  InvoiceProfile,
  InvoiceProfileInput,
  InvoiceRequest,
  InvoiceRequestInput,
} from "@/types";

export const invoicesAPI = {
  listProfiles() {
    return apiClient.get<InvoiceProfile[]>("/user/invoices/profiles");
  },

  createProfile(data: InvoiceProfileInput) {
    return apiClient.post<InvoiceProfile>("/user/invoices/profiles", data);
  },

  updateProfile(id: number, data: InvoiceProfileInput) {
    return apiClient.put<InvoiceProfile>(`/user/invoices/profiles/${id}`, data);
  },

  deleteProfile(id: number) {
    return apiClient.delete<{ deleted: boolean }>(
      `/user/invoices/profiles/${id}`,
    );
  },

  setDefaultProfile(id: number) {
    return apiClient.post<InvoiceProfile>(
      `/user/invoices/profiles/${id}/default`,
    );
  },

  listEligibleSources(params?: { page?: number; page_size?: number }) {
    return apiClient.get<BasePaginationResponse<InvoiceEligibleSource>>(
      "/user/invoices/eligible-sources",
      { params },
    );
  },

  listRequests(params?: {
    page?: number;
    page_size?: number;
    status?: string;
    keyword?: string;
  }) {
    return apiClient.get<BasePaginationResponse<InvoiceRequest>>(
      "/user/invoices/requests",
      { params },
    );
  },

  createRequest(data: InvoiceRequestInput) {
    return apiClient.post<InvoiceRequest>("/user/invoices/requests", data);
  },

  getRequest(id: number) {
    return apiClient.get<InvoiceRequest>(`/user/invoices/requests/${id}`);
  },

  cancelRequest(id: number) {
    return apiClient.post<InvoiceRequest>(
      `/user/invoices/requests/${id}/cancel`,
    );
  },
};

export default invoicesAPI;
