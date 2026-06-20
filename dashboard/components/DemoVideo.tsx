"use client";

import { useState } from "react";
import { Play } from "lucide-react";

interface DemoVideoProps {
  src: string;
  alt: string;
  placeholderText?: string;
}

export default function DemoVideo({ src, alt, placeholderText }: DemoVideoProps) {
  const [hasError, setHasError] = useState(false);

  if (hasError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center text-gray-500 w-full h-full min-h-[200px] bg-black/40">
        <Play className="w-12 h-12 text-white/10 mb-3" />
        <p className="font-bold text-gray-400 text-sm">{alt}</p>
        <p className="text-[10px] text-gray-600 mt-1">[En attente d'enregistrement {src.split('/').pop()}]</p>
      </div>
    );
  }

  return (
    <img
      src={src}
      alt={alt}
      className="w-full h-full object-cover"
      onError={() => setHasError(true)}
    />
  );
}
