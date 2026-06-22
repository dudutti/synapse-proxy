"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { clsx } from "clsx";

interface BlogPaginationProps {
  currentPage: number;
  totalPages: number;
}

export default function BlogPagination({ currentPage, totalPages }: BlogPaginationProps) {
  const router = useRouter();
  const searchParams = useSearchParams();

  if (totalPages <= 1) return null;

  const handlePageChange = (page: number) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("page", page.toString());
    const query = params.toString();
    router.push(`/blog${query ? `?${query}` : ""}`);
  };

  const pages = Array.from({ length: totalPages }, (_, i) => i + 1);

  return (
    <div className="flex justify-center items-center gap-2 mt-16 pb-8">
      <button
        onClick={() => handlePageChange(currentPage - 1)}
        disabled={currentPage === 1}
        className="w-10 h-10 flex items-center justify-center rounded-xl bg-[#0f0f11] border border-white/5 text-gray-400 hover:text-white hover:border-emerald-500/50 transition-all disabled:opacity-50 disabled:pointer-events-none"
      >
        <ChevronLeft className="w-5 h-5" />
      </button>

      {pages.map((page) => (
        <button
          key={page}
          onClick={() => handlePageChange(page)}
          className={clsx(
            "w-10 h-10 flex items-center justify-center rounded-xl text-sm font-medium transition-all",
            currentPage === page
              ? "bg-emerald-500 text-white shadow-[0_0_20px_rgba(16,185,129,0.3)]"
              : "bg-[#0f0f11] text-gray-400 border border-white/5 hover:border-emerald-500/50 hover:text-white"
          )}
        >
          {page}
        </button>
      ))}

      <button
        onClick={() => handlePageChange(currentPage + 1)}
        disabled={currentPage === totalPages}
        className="w-10 h-10 flex items-center justify-center rounded-xl bg-[#0f0f11] border border-white/5 text-gray-400 hover:text-white hover:border-emerald-500/50 transition-all disabled:opacity-50 disabled:pointer-events-none"
      >
        <ChevronRight className="w-5 h-5" />
      </button>
    </div>
  );
}
