"use client";

import React, { forwardRef } from "react";
import Globe, { GlobeProps } from "react-globe.gl";

const GlobeWrapper = ({ innerRef, ...props }: any) => {
  return <Globe ref={innerRef} {...props} />;
};

export default GlobeWrapper;
