import { useState, useEffect } from "react";

export function useWindowWidth(): number {
  const [width, setWidth] = useState(() => window.innerWidth);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;
    const handler = () => {
      clearTimeout(timer);
      timer = setTimeout(() => setWidth(window.innerWidth), 150);
    };
    window.addEventListener("resize", handler);
    return () => {
      clearTimeout(timer);
      window.removeEventListener("resize", handler);
    };
  }, []);

  return width;
}
