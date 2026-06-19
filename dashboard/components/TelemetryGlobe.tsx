"use client";

import React, { useEffect, useState, memo, useRef } from "react";
import dynamic from "next/dynamic";

const Globe = dynamic(() => import("./GlobeWrapper"), { ssr: false });

const TelemetryGlobe = memo(() => {
  const globeRef = useRef<any>();
  const [dimensions, setDimensions] = useState({ width: 300, height: 300 });
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Auto-rotate
    if (globeRef.current) {
      globeRef.current.controls().autoRotate = true;
      globeRef.current.controls().autoRotateSpeed = 1.0;
      globeRef.current.controls().enableZoom = false;
    }

    // Handle responsive sizing
    const updateSize = () => {
      if (containerRef.current) {
        setDimensions({
          width: containerRef.current.clientWidth,
          height: containerRef.current.clientHeight
        });
      }
    };
    
    updateSize();
    window.addEventListener('resize', updateSize);
    
    // Slight delay to ensure parent container has sized properly
    setTimeout(updateSize, 100);

    return () => window.removeEventListener('resize', updateSize);
  }, []);

  const markers = [
    { location: [37.7595, -122.4367], size: 0.05, color: [0.1, 0.8, 0.8] },
    { location: [40.7128, -74.0060], size: 0.05, color: [0.1, 0.8, 0.8] },
    { location: [51.5074, -0.1278], size: 0.05, color: [0.1, 0.8, 0.8] },
    { location: [48.8566, 2.3522], size: 0.05, color: [0.1, 0.8, 0.8] },
    { location: [35.6762, 139.6503], size: 0.05, color: [0.1, 0.8, 0.8] },
    { location: [1.3521, 103.8198], size: 0.05, color: [0.1, 0.8, 0.8] },
    { location: [-33.8688, 151.2093], size: 0.05, color: [0.1, 0.8, 0.8] },
  ];

  // Map markers to glowing rings
  const ringsData = markers.map(m => ({
    lat: m.location[0],
    lng: m.location[1],
    maxR: m.size * 80, // Scale up for visibility
    propagationSpeed: 2,
    repeatPeriod: 1000 + Math.random() * 1000, // Randomize pulses
    color: `rgb(${Math.round(m.color[0]*255)}, ${Math.round(m.color[1]*255)}, ${Math.round(m.color[2]*255)})`
  }));

  return (
    <div className="relative w-full h-full flex items-center justify-center" ref={containerRef}>
      <Globe
        innerRef={globeRef}
        width={dimensions.width}
        height={dimensions.height}
        backgroundColor="rgba(0,0,0,0)"
        globeImageUrl="//unpkg.com/three-globe/example/img/earth-dark.jpg"
        ringsData={ringsData}
        ringColor="color"
        ringMaxRadius="maxR"
        ringPropagationSpeed="propagationSpeed"
        ringRepeatPeriod="repeatPeriod"
      />
    </div>
  );
});

TelemetryGlobe.displayName = "TelemetryGlobe";

export default TelemetryGlobe;
