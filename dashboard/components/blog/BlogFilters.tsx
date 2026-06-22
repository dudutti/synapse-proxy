"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { clsx } from "clsx";

interface BlogFiltersProps {
  categories: string[];
  lang: string;
}

export default function BlogFilters({ categories, lang }: BlogFiltersProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const currentCategory = searchParams.get("category") || "all";

  const t = {
    fr: { all: "Tous" },
    en: { all: "All" }
  }[lang] || { all: "Tous" };

  const handleSelect = (cat: string) => {
    const params = new URLSearchParams(searchParams.toString());
    if (cat === "all") {
      params.delete("category");
    } else {
      params.set("category", cat);
    }
    // Reset page on filter change
    params.delete("page");
    
    const query = params.toString();
    router.push(`/blog${query ? `?${query}` : ""}`, { scroll: false });
  };

  return (
    <div className="w-full overflow-x-auto pb-4 mb-8 scrollbar-hide">
      <div className="flex gap-3 min-w-max">
        <button
          onClick={() => handleSelect("all")}
          className={clsx(
            "px-6 py-2.5 rounded-full text-sm font-medium transition-all",
            currentCategory === "all"
              ? "bg-emerald-500 text-white shadow-[0_0_20px_rgba(16,185,129,0.3)]"
              : "bg-[#0f0f11] text-gray-400 border border-white/5 hover:border-emerald-500/50 hover:text-white"
          )}
        >
          {t.all}
        </button>
        {categories.map((cat) => (
          <button
            key={cat}
            onClick={() => handleSelect(cat)}
            className={clsx(
              "px-6 py-2.5 rounded-full text-sm font-medium transition-all capitalize",
              currentCategory === cat
                ? "bg-emerald-500 text-white shadow-[0_0_20px_rgba(16,185,129,0.3)]"
                : "bg-[#0f0f11] text-gray-400 border border-white/5 hover:border-emerald-500/50 hover:text-white"
            )}
          >
            {cat}
          </button>
        ))}
      </div>
    </div>
  );
}
