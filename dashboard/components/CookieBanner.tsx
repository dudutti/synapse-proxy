"use client";

import { useState, useEffect } from "react";
import { X, Cookie } from "lucide-react";

export function CookieBanner() {
  const [show, setShow] = useState(false);

  useEffect(() => {
    const consent = localStorage.getItem("cookieConsent");
    if (!consent) {
      setShow(true);
    } else if (consent === "accepted") {
      updateConsent(true);
    }
  }, []);

  const updateConsent = (granted: boolean) => {
    window.dataLayer = window.dataLayer || [];
    function gtag(){window.dataLayer.push(arguments);}
    // @ts-ignore
    gtag('consent', 'update', {
      'ad_storage': granted ? 'granted' : 'denied',
      'analytics_storage': granted ? 'granted' : 'denied'
    });
  };

  const handleAccept = () => {
    localStorage.setItem("cookieConsent", "accepted");
    setShow(false);
    updateConsent(true);
    // Push an event to GTM so it can trigger tags that wait for consent
    window.dataLayer?.push({ event: "cookie_consent_accepted" });
  };

  const handleDecline = () => {
    localStorage.setItem("cookieConsent", "declined");
    setShow(false);
    updateConsent(false);
  };

  if (!show) return null;

  return (
    <div className="fixed bottom-0 left-0 right-0 z-50 p-4 md:p-6 bg-[#0a0a0c]/95 backdrop-blur-md border-t border-white/10 shadow-[0_-10px_40px_rgba(0,0,0,0.5)] flex flex-col md:flex-row items-start md:items-center justify-between gap-6 animate-in slide-in-from-bottom-10 duration-500">
      <div className="flex items-start gap-4 flex-1 max-w-4xl">
        <div className="p-3 bg-emerald-500/10 rounded-xl shrink-0 hidden sm:block">
          <Cookie className="w-6 h-6 text-emerald-400" />
        </div>
        <div className="text-sm text-gray-300">
          <p className="font-bold text-white mb-1 flex items-center gap-2">
            <Cookie className="w-4 h-4 text-emerald-400 sm:hidden" />
            Respect de votre vie privée
          </p>
          Nous utilisons des cookies (via Google Tag Manager) pour analyser le trafic et comprendre comment vous utilisez notre plateforme. Ces données analytiques nous permettent d'améliorer continuellement Synapse Proxy. Vous pouvez accepter ou refuser ces cookies à tout moment.
        </div>
      </div>
      
      <div className="flex items-center gap-3 shrink-0 w-full md:w-auto">
        <button 
          onClick={handleDecline}
          className="flex-1 md:flex-none px-5 py-2.5 rounded-xl bg-white/5 hover:bg-white/10 text-white font-bold text-sm transition-all border border-white/10"
        >
          Refuser
        </button>
        <button 
          onClick={handleAccept}
          className="flex-1 md:flex-none px-5 py-2.5 rounded-xl bg-emerald-500 hover:bg-emerald-400 text-black font-bold text-sm transition-all shadow-[0_0_20px_rgba(16,185,129,0.3)]"
        >
          Accepter
        </button>
      </div>

      <button 
        onClick={handleDecline}
        className="absolute top-4 right-4 md:hidden text-gray-500 hover:text-white"
        aria-label="Fermer"
      >
        <X className="w-5 h-5" />
      </button>
    </div>
  );
}
